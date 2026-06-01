package controller

import (
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	leapcoreMachineIDPrefix  = "FGLC_"
	leapcoreMachineIDMaxLen  = 255
	leapcoreMachineTokenName = "leapcore-machine"
)

var leapcoreMachineIDPayloadPattern = regexp.MustCompile(`^[A-Za-z0-9+/]+={0,2}$`)
var leapcoreMachineTokenLocks sync.Map

type leapcoreRegisterRequest struct {
	MachineID string `json:"machine_id"`
}

type leapcoreMachineUserRequest struct {
	MachineID string `json:"machine_id"`
}

type leapcoreMachineUserResponse struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	MachineID   string `json:"machine_id"`
	Password    string `json:"password"`
	Created     bool   `json:"created"`
	DisplayName string `json:"display_name,omitempty"`
	Group       string `json:"group,omitempty"`
}

func LeapCoreRegister(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")

	if !validateLeapcoreHelperKey(c.GetHeader("X-Helper-Key")) {
		if getLeapcoreHelperKey() == "" {
			leapcoreEnvelope(c, http.StatusServiceUnavailable, false, "leapcore helper key is not configured", nil)
			return
		}
		leapcoreEnvelope(c, http.StatusUnauthorized, false, "invalid helper key", nil)
		return
	}

	var req leapcoreRegisterRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		leapcoreEnvelope(c, http.StatusBadRequest, false, "invalid request body", nil)
		return
	}

	machineID, err := normalizeLeapcoreMachineID(req.MachineID)
	if err != nil {
		leapcoreEnvelope(c, http.StatusBadRequest, false, err.Error(), nil)
		return
	}

	username := deriveLeapcoreUsername(machineID)
	user, err := getLeapcoreMachineUser(username)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			leapcoreEnvelope(c, http.StatusForbidden, false, "machine user is not provisioned", nil)
			return
		}
		common.SysLog("LeapCore register failed to query user: " + err.Error())
		leapcoreEnvelope(c, http.StatusInternalServerError, false, "database error", nil)
		return
	}
	if err := validateLeapcoreMachineUser(user, machineID); err != nil {
		leapcoreEnvelope(c, http.StatusForbidden, false, err.Error(), nil)
		return
	}

	token, err := getOrCreateLeapcoreMachineToken(user)
	if err != nil {
		switch err {
		case errLeapcoreMachineTokenUnusable:
			leapcoreEnvelope(c, http.StatusForbidden, false, err.Error(), nil)
		default:
			common.SysLog("LeapCore register failed to resolve token: " + err.Error())
			leapcoreEnvelope(c, http.StatusInternalServerError, false, "failed to resolve machine token", nil)
		}
		return
	}

	leapcoreEnvelope(c, http.StatusOK, true, "", gin.H{
		"key": formatLeapcoreTokenKey(token.Key),
	})
}

func CreateLeapCoreMachineUser(c *gin.Context) {
	var req leapcoreMachineUserRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}

	machineID, err := normalizeLeapcoreMachineInput(req.MachineID)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}

	username := deriveLeapcoreUsername(machineID)
	password := deriveLeapcorePassword(machineID)
	adminInfo := buildLeapcoreAdminInfo(c)

	var existing model.User
	err = model.DB.Unscoped().Where("username = ?", username).First(&existing).Error
	if err == nil {
		if existing.DeletedAt.Valid {
			common.ApiErrorMsg(c, "machine user exists but is deleted")
			return
		}
		if existing.Remark != machineID {
			common.ApiErrorMsg(c, "machine user already exists with a different remark")
			return
		}
		model.RecordLogWithAdminInfo(existing.Id, model.LogTypeManage,
			fmt.Sprintf("管理员复用 LeapCore 机器用户 (machine_id=%s)", truncateLeapcoreMachineID(machineID)),
			adminInfo)
		common.ApiSuccess(c, leapcoreMachineUserResponse{
			ID:          existing.Id,
			Username:    existing.Username,
			MachineID:   machineID,
			Password:    password,
			Created:     false,
			DisplayName: existing.DisplayName,
			Group:       existing.Group,
		})
		return
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		common.SysLog("LeapCore machine user lookup failed: " + err.Error())
		common.ApiErrorMsg(c, "database error")
		return
	}

	user := model.User{
		Username:    username,
		Password:    password,
		DisplayName: username,
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Remark:      machineID,
	}
	if err := user.Insert(0); err != nil {
		common.ApiError(c, err)
		return
	}

	model.RecordLogWithAdminInfo(user.Id, model.LogTypeManage,
		fmt.Sprintf("管理员预置 LeapCore 机器用户 (machine_id=%s)", truncateLeapcoreMachineID(machineID)),
		adminInfo)

	common.ApiSuccess(c, leapcoreMachineUserResponse{
		ID:          user.Id,
		Username:    user.Username,
		MachineID:   machineID,
		Password:    password,
		Created:     true,
		DisplayName: user.DisplayName,
		Group:       user.Group,
	})
}

func buildLeapcoreAdminInfo(c *gin.Context) map[string]interface{} {
	return map[string]interface{}{
		"admin_id":       c.GetInt("id"),
		"admin_username": c.GetString("username"),
	}
}

func truncateLeapcoreMachineID(machineID string) string {
	const maxLen = 32
	if len(machineID) <= maxLen {
		return machineID
	}
	return machineID[:maxLen] + "..."
}

func validateLeapcoreHelperKey(provided string) bool {
	expected := getLeapcoreHelperKey()
	provided = strings.TrimSpace(provided)
	if expected == "" || provided == "" {
		return false
	}
	expectedHash := common.Sha256Raw([]byte(expected))
	providedHash := common.Sha256Raw([]byte(provided))
	return subtle.ConstantTimeCompare(expectedHash, providedHash) == 1
}

func getLeapcoreHelperKey() string {
	return strings.TrimSpace(common.LeapCoreHelperKey)
}

func normalizeLeapcoreMachineInput(raw string) (string, error) {
	input := strings.TrimSpace(raw)
	if input == "" {
		return "", fmt.Errorf("machine_id is required")
	}
	if strings.HasPrefix(input, leapcoreMachineIDPrefix) {
		return normalizeLeapcoreMachineID(input)
	}
	decoded, err := base64.StdEncoding.DecodeString(input)
	if err == nil {
		decodedInput := strings.TrimSpace(string(decoded))
		if strings.HasPrefix(decodedInput, leapcoreMachineIDPrefix) {
			return normalizeLeapcoreMachineID(decodedInput)
		}
	}
	return normalizeLeapcoreMachineID(leapcoreMachineIDPrefix + base64.StdEncoding.EncodeToString([]byte(input)))
}

func normalizeLeapcoreMachineID(raw string) (string, error) {
	machineID := strings.TrimSpace(raw)
	if machineID == "" {
		return "", fmt.Errorf("machine_id is required")
	}
	if len(machineID) > leapcoreMachineIDMaxLen {
		return "", fmt.Errorf("machine_id is too long")
	}
	if !strings.HasPrefix(machineID, leapcoreMachineIDPrefix) {
		return "", fmt.Errorf("machine_id must start with %s", leapcoreMachineIDPrefix)
	}
	payload := strings.TrimPrefix(machineID, leapcoreMachineIDPrefix)
	if payload == "" || !leapcoreMachineIDPayloadPattern.MatchString(payload) {
		return "", fmt.Errorf("machine_id payload is not valid base64")
	}
	if _, err := base64.StdEncoding.DecodeString(payload); err != nil {
		return "", fmt.Errorf("machine_id payload is not valid base64")
	}
	return machineID, nil
}

func deriveLeapcoreUsername(machineID string) string {
	return leapcoreMachineIDPrefix + deriveLeapcoreMachineHash(machineID)[:15]
}

func deriveLeapcorePassword(machineID string) string {
	return deriveLeapcoreMachineHash(machineID)[:15]
}

func deriveLeapcoreMachineHash(machineID string) string {
	return fmt.Sprintf("%x", common.Sha256Raw([]byte(machineID)))
}

func getLeapcoreMachineUser(username string) (*model.User, error) {
	var user model.User
	if err := model.DB.Where("username = ?", username).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func validateLeapcoreMachineUser(user *model.User, machineID string) error {
	if user.Status != common.UserStatusEnabled {
		return fmt.Errorf("machine user is disabled")
	}
	if user.Role != common.RoleCommonUser {
		return fmt.Errorf("machine user role is not allowed")
	}
	if user.Remark != machineID {
		return fmt.Errorf("machine_id does not match machine user remark")
	}
	return nil
}

var errLeapcoreMachineTokenUnusable = fmt.Errorf("leapcore machine token exists but is not usable")

func getOrCreateLeapcoreMachineToken(user *model.User) (*model.Token, error) {
	lockValue, _ := leapcoreMachineTokenLocks.LoadOrStore(user.Id, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	token, exists, err := getLatestUsableLeapcoreMachineToken(user.Id)
	if err != nil {
		return nil, err
	}
	if token != nil {
		return token, nil
	}
	if exists {
		return nil, errLeapcoreMachineTokenUnusable
	}
	return createLeapcoreMachineToken(user)
}

func getLatestUsableLeapcoreMachineToken(userID int) (*model.Token, bool, error) {
	var tokens []model.Token
	if err := model.DB.Where("user_id = ? AND name = ?", userID, leapcoreMachineTokenName).
		Order("id desc").
		Find(&tokens).Error; err != nil {
		return nil, false, err
	}
	for i := range tokens {
		if isLeapcoreTokenUsable(&tokens[i]) {
			return &tokens[i], true, nil
		}
	}
	return nil, len(tokens) > 0, nil
}

func isLeapcoreTokenUsable(token *model.Token) bool {
	if token.Status != common.TokenStatusEnabled {
		return false
	}
	if token.ExpiredTime != -1 && token.ExpiredTime < common.GetTimestamp() {
		return false
	}
	return token.UnlimitedQuota || token.RemainQuota > 0
}

func createLeapcoreMachineToken(user *model.User) (*model.Token, error) {
	key, err := common.GenerateKey()
	if err != nil {
		return nil, err
	}
	token := &model.Token{
		UserId:             user.Id,
		Name:               leapcoreMachineTokenName,
		Key:                key,
		Status:             common.TokenStatusEnabled,
		CreatedTime:        common.GetTimestamp(),
		AccessedTime:       common.GetTimestamp(),
		ExpiredTime:        -1,
		RemainQuota:        0,
		UnlimitedQuota:     true,
		ModelLimitsEnabled: false,
		ModelLimits:        "",
	}
	if setting.DefaultUseAutoGroup {
		token.Group = "auto"
	}
	if err := token.Insert(); err != nil {
		return nil, err
	}
	model.RecordLog(user.Id, model.LogTypeSystem, fmt.Sprintf("LeapCore machine token created (token_id=%d, username=%s)", token.Id, user.Username))
	return token, nil
}

func formatLeapcoreTokenKey(key string) string {
	key = strings.TrimSpace(key)
	if strings.HasPrefix(key, "sk-") {
		return key
	}
	return "sk-" + key
}

func leapcoreEnvelope(c *gin.Context, status int, success bool, message string, data any) {
	c.AbortWithStatusJSON(status, gin.H{
		"success": success,
		"message": message,
		"data":    data,
	})
}
