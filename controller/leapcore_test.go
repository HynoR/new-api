package controller

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const leapcoreTestHelperKey = "test-helper-key"

func setLeapcoreHelperKey(t *testing.T, helperKey string) {
	t.Helper()

	originalHelperKey := common.LeapCoreHelperKey
	common.LeapCoreHelperKey = helperKey
	t.Cleanup(func() {
		common.LeapCoreHelperKey = originalHelperKey
	})
}

func setupLeapcoreControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db := openTokenControllerTestDB(t)
	if err := db.AutoMigrate(&model.User{}, &model.Token{}, &model.Log{}, &model.Option{}); err != nil {
		t.Fatalf("failed to migrate leapcore test tables: %v", err)
	}
	return db
}

func leapcoreMachineID(raw string) string {
	return leapcoreMachineIDPrefix + base64.StdEncoding.EncodeToString([]byte(raw))
}

func seedLeapcoreMachineUser(t *testing.T, db *gorm.DB, machineID string, mutate func(*model.User)) *model.User {
	t.Helper()

	username := deriveLeapcoreUsername(machineID)
	user := &model.User{
		Username:    username,
		Password:    "password",
		DisplayName: username,
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AffCode:     strings.TrimPrefix(username, leapcoreMachineIDPrefix),
		Remark:      machineID,
	}
	if mutate != nil {
		mutate(user)
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed leapcore machine user: %v", err)
	}
	return user
}

func seedLeapcoreMachineToken(t *testing.T, db *gorm.DB, userID int, status int, rawKey string) *model.Token {
	t.Helper()

	token := &model.Token{
		UserId:             userID,
		Name:               leapcoreMachineTokenName,
		Key:                rawKey,
		Status:             status,
		CreatedTime:        1,
		AccessedTime:       1,
		ExpiredTime:        -1,
		RemainQuota:        0,
		UnlimitedQuota:     true,
		ModelLimitsEnabled: false,
	}
	if err := db.Create(token).Error; err != nil {
		t.Fatalf("failed to seed leapcore machine token: %v", err)
	}
	return token
}

func countLeapcoreMachineTokens(t *testing.T, db *gorm.DB, userID int) int64 {
	t.Helper()

	var count int64
	if err := db.Model(&model.Token{}).Where("user_id = ? AND name = ?", userID, leapcoreMachineTokenName).Count(&count).Error; err != nil {
		t.Fatalf("failed to count leapcore machine tokens: %v", err)
	}
	return count
}

func newLeapcoreRegisterContext(t *testing.T, machineID string, helperKey string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	body, err := common.Marshal(gin.H{"machine_id": machineID})
	if err != nil {
		t.Fatalf("failed to marshal leapcore request: %v", err)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/leapcore/register", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	if helperKey != "" {
		ctx.Request.Header.Set("X-Helper-Key", helperKey)
	}
	return ctx, recorder
}

func newLeapcoreMachineUserContext(t *testing.T, machineID string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	body, err := common.Marshal(gin.H{"machine_id": machineID})
	if err != nil {
		t.Fatalf("failed to marshal leapcore machine user request: %v", err)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/leapcore/machine", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	return ctx, recorder
}

func decodeLeapcoreAPIResponse(t *testing.T, recorder *httptest.ResponseRecorder) tokenAPIResponse {
	t.Helper()

	var response tokenAPIResponse
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode leapcore response: %v", err)
	}
	return response
}

func assertLeapcoreError(t *testing.T, recorder *httptest.ResponseRecorder, status int) {
	t.Helper()

	if recorder.Code != status {
		t.Fatalf("expected status %d, got %d with body %s", status, recorder.Code, recorder.Body.String())
	}
	response := decodeLeapcoreAPIResponse(t, recorder)
	if response.Success {
		t.Fatalf("expected error response, got success body %s", recorder.Body.String())
	}
}

func decodeLeapcoreMachineUserResponse(t *testing.T, recorder *httptest.ResponseRecorder) leapcoreMachineUserResponse {
	t.Helper()

	response := decodeLeapcoreAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got body %s", recorder.Body.String())
	}
	var data leapcoreMachineUserResponse
	if err := common.Unmarshal(response.Data, &data); err != nil {
		t.Fatalf("failed to decode leapcore machine user response: %v", err)
	}
	return data
}

func TestLeapCoreRegisterRejectsMissingConfiguredHelperKey(t *testing.T) {
	setupLeapcoreControllerTestDB(t)
	setLeapcoreHelperKey(t, "")

	ctx, recorder := newLeapcoreRegisterContext(t, leapcoreMachineID("missing-config"), leapcoreTestHelperKey)
	LeapCoreRegister(ctx)

	assertLeapcoreError(t, recorder, http.StatusServiceUnavailable)
}

func TestLeapCoreRegisterRejectsMissingHelperKey(t *testing.T) {
	setupLeapcoreControllerTestDB(t)
	setLeapcoreHelperKey(t, leapcoreTestHelperKey)

	ctx, recorder := newLeapcoreRegisterContext(t, leapcoreMachineID("missing-helper"), "")
	LeapCoreRegister(ctx)

	assertLeapcoreError(t, recorder, http.StatusUnauthorized)
}

func TestLeapCoreRegisterRejectsWrongHelperKey(t *testing.T) {
	setupLeapcoreControllerTestDB(t)
	setLeapcoreHelperKey(t, leapcoreTestHelperKey)

	ctx, recorder := newLeapcoreRegisterContext(t, leapcoreMachineID("wrong-helper"), "wrong-helper")
	LeapCoreRegister(ctx)

	assertLeapcoreError(t, recorder, http.StatusUnauthorized)
}

func TestLeapCoreRegisterUsesHelperKeyFromOption(t *testing.T) {
	db := setupLeapcoreControllerTestDB(t)
	originalOptionMap := common.OptionMap
	originalHelperKey := common.LeapCoreHelperKey
	common.OptionMap = map[string]string{}
	common.LeapCoreHelperKey = ""
	t.Cleanup(func() {
		common.OptionMap = originalOptionMap
		common.LeapCoreHelperKey = originalHelperKey
	})
	if err := model.UpdateOption("LeapCoreHelperKey", leapcoreTestHelperKey); err != nil {
		t.Fatalf("failed to update LeapCoreHelperKey option: %v", err)
	}

	machineID := leapcoreMachineID("option-helper-key")
	seedLeapcoreMachineUser(t, db, machineID, nil)

	ctx, recorder := newLeapcoreRegisterContext(t, machineID, leapcoreTestHelperKey)
	LeapCoreRegister(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", recorder.Code, recorder.Body.String())
	}
}

func TestLeapCoreRegisterRejectsInvalidMachineID(t *testing.T) {
	setupLeapcoreControllerTestDB(t)
	setLeapcoreHelperKey(t, leapcoreTestHelperKey)

	testCases := []string{
		"",
		"WRONG_" + base64.StdEncoding.EncodeToString([]byte("fingerprint")),
		leapcoreMachineIDPrefix + "not base64***",
		leapcoreMachineIDPrefix + strings.Repeat("a", leapcoreMachineIDMaxLen),
	}
	for _, machineID := range testCases {
		ctx, recorder := newLeapcoreRegisterContext(t, machineID, leapcoreTestHelperKey)
		LeapCoreRegister(ctx)
		assertLeapcoreError(t, recorder, http.StatusBadRequest)
	}
}

func TestLeapCoreRegisterRejectsMissingMachineUser(t *testing.T) {
	setupLeapcoreControllerTestDB(t)
	setLeapcoreHelperKey(t, leapcoreTestHelperKey)

	ctx, recorder := newLeapcoreRegisterContext(t, leapcoreMachineID("missing-user"), leapcoreTestHelperKey)
	LeapCoreRegister(ctx)

	assertLeapcoreError(t, recorder, http.StatusForbidden)
}

func TestLeapCoreRegisterRejectsRemarkMismatch(t *testing.T) {
	db := setupLeapcoreControllerTestDB(t)
	setLeapcoreHelperKey(t, leapcoreTestHelperKey)
	machineID := leapcoreMachineID("remark-mismatch")
	seedLeapcoreMachineUser(t, db, machineID, func(user *model.User) {
		user.Remark = leapcoreMachineID("different-machine")
	})

	ctx, recorder := newLeapcoreRegisterContext(t, machineID, leapcoreTestHelperKey)
	LeapCoreRegister(ctx)

	assertLeapcoreError(t, recorder, http.StatusForbidden)
}

func TestLeapCoreRegisterRejectsDisabledMachineUser(t *testing.T) {
	db := setupLeapcoreControllerTestDB(t)
	setLeapcoreHelperKey(t, leapcoreTestHelperKey)
	machineID := leapcoreMachineID("disabled-user")
	seedLeapcoreMachineUser(t, db, machineID, func(user *model.User) {
		user.Status = common.UserStatusDisabled
	})

	ctx, recorder := newLeapcoreRegisterContext(t, machineID, leapcoreTestHelperKey)
	LeapCoreRegister(ctx)

	assertLeapcoreError(t, recorder, http.StatusForbidden)
}

func TestLeapCoreRegisterRejectsNonCommonMachineUser(t *testing.T) {
	db := setupLeapcoreControllerTestDB(t)
	setLeapcoreHelperKey(t, leapcoreTestHelperKey)
	machineID := leapcoreMachineID("admin-user")
	seedLeapcoreMachineUser(t, db, machineID, func(user *model.User) {
		user.Role = common.RoleAdminUser
	})

	ctx, recorder := newLeapcoreRegisterContext(t, machineID, leapcoreTestHelperKey)
	LeapCoreRegister(ctx)

	assertLeapcoreError(t, recorder, http.StatusForbidden)
}

func TestLeapCoreRegisterCreatesMachineToken(t *testing.T) {
	db := setupLeapcoreControllerTestDB(t)
	setLeapcoreHelperKey(t, leapcoreTestHelperKey)
	originalDefaultUseAutoGroup := setting.DefaultUseAutoGroup
	setting.DefaultUseAutoGroup = true
	t.Cleanup(func() {
		setting.DefaultUseAutoGroup = originalDefaultUseAutoGroup
	})

	machineID := leapcoreMachineID("create-token")
	user := seedLeapcoreMachineUser(t, db, machineID, nil)

	ctx, recorder := newLeapcoreRegisterContext(t, machineID, leapcoreTestHelperKey)
	LeapCoreRegister(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected no-store cache header, got %q", recorder.Header().Get("Cache-Control"))
	}
	response := decodeLeapcoreAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message %q", response.Message)
	}

	var keyData tokenKeyResponse
	if err := common.Unmarshal(response.Data, &keyData); err != nil {
		t.Fatalf("failed to decode key response: %v", err)
	}
	if !strings.HasPrefix(keyData.Key, "sk-") {
		t.Fatalf("expected sk-prefixed key, got %q", keyData.Key)
	}

	var token model.Token
	if err := db.First(&token, "user_id = ? AND name = ?", user.Id, leapcoreMachineTokenName).Error; err != nil {
		t.Fatalf("failed to fetch created token: %v", err)
	}
	if keyData.Key != "sk-"+token.Key {
		t.Fatalf("expected returned key to match created token")
	}
	if token.Status != common.TokenStatusEnabled || !token.UnlimitedQuota || token.RemainQuota != 0 || token.ExpiredTime != -1 {
		t.Fatalf("created token has unexpected limits/status: %#v", token)
	}
	if token.Group != "auto" {
		t.Fatalf("expected created token to use auto group, got %q", token.Group)
	}
}

func TestLeapCoreRegisterReusesExistingMachineToken(t *testing.T) {
	db := setupLeapcoreControllerTestDB(t)
	setLeapcoreHelperKey(t, leapcoreTestHelperKey)
	machineID := leapcoreMachineID("reuse-token")
	user := seedLeapcoreMachineUser(t, db, machineID, nil)
	token := seedLeapcoreMachineToken(t, db, user.Id, common.TokenStatusEnabled, "existing-token-key")

	for i := 0; i < 2; i++ {
		ctx, recorder := newLeapcoreRegisterContext(t, machineID, leapcoreTestHelperKey)
		LeapCoreRegister(ctx)
		if recorder.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d with body %s", recorder.Code, recorder.Body.String())
		}
		response := decodeLeapcoreAPIResponse(t, recorder)
		var keyData tokenKeyResponse
		if err := common.Unmarshal(response.Data, &keyData); err != nil {
			t.Fatalf("failed to decode key response: %v", err)
		}
		if keyData.Key != "sk-"+token.Key {
			t.Fatalf("expected existing key %q, got %q", "sk-"+token.Key, keyData.Key)
		}
	}

	if count := countLeapcoreMachineTokens(t, db, user.Id); count != 1 {
		t.Fatalf("expected existing token to be reused, got %d tokens", count)
	}
}

func TestLeapCoreRegisterDoesNotBypassUnusableMachineToken(t *testing.T) {
	db := setupLeapcoreControllerTestDB(t)
	setLeapcoreHelperKey(t, leapcoreTestHelperKey)

	testCases := []struct {
		name   string
		mutate func(*model.Token)
	}{
		{
			name: "disabled-token",
			mutate: func(token *model.Token) {
				token.Status = common.TokenStatusDisabled
			},
		},
		{
			name: "expired-token",
			mutate: func(token *model.Token) {
				token.ExpiredTime = common.GetTimestamp() - 1
			},
		},
		{
			name: "exhausted-limited-token",
			mutate: func(token *model.Token) {
				token.UnlimitedQuota = false
				token.RemainQuota = 0
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			machineID := leapcoreMachineID(tc.name)
			user := seedLeapcoreMachineUser(t, db, machineID, nil)
			token := seedLeapcoreMachineToken(t, db, user.Id, common.TokenStatusEnabled, tc.name+"-key")
			tc.mutate(token)
			if err := db.Save(token).Error; err != nil {
				t.Fatalf("failed to mutate leapcore machine token: %v", err)
			}

			ctx, recorder := newLeapcoreRegisterContext(t, machineID, leapcoreTestHelperKey)
			LeapCoreRegister(ctx)

			assertLeapcoreError(t, recorder, http.StatusForbidden)
			if count := countLeapcoreMachineTokens(t, db, user.Id); count != 1 {
				t.Fatalf("expected unusable token not to be bypassed, got %d tokens", count)
			}
		})
	}
}

func TestLeapCoreRegisterConcurrentCreateIsIdempotent(t *testing.T) {
	db := setupLeapcoreControllerTestDB(t)
	setLeapcoreHelperKey(t, leapcoreTestHelperKey)
	machineID := leapcoreMachineID("concurrent-create")
	user := seedLeapcoreMachineUser(t, db, machineID, nil)

	const requestCount = 12
	var wg sync.WaitGroup
	wg.Add(requestCount)
	statuses := make(chan int, requestCount)
	keys := make(chan string, requestCount)

	for i := 0; i < requestCount; i++ {
		go func() {
			defer wg.Done()
			ctx, recorder := newLeapcoreRegisterContext(t, machineID, leapcoreTestHelperKey)
			LeapCoreRegister(ctx)
			statuses <- recorder.Code
			if recorder.Code != http.StatusOK {
				return
			}
			response := decodeLeapcoreAPIResponse(t, recorder)
			var keyData tokenKeyResponse
			if err := common.Unmarshal(response.Data, &keyData); err != nil {
				t.Errorf("failed to decode key response: %v", err)
				return
			}
			keys <- keyData.Key
		}()
	}

	wg.Wait()
	close(statuses)
	close(keys)

	for status := range statuses {
		if status != http.StatusOK {
			t.Fatalf("expected all concurrent requests to succeed, got status %d", status)
		}
	}

	var firstKey string
	for key := range keys {
		if !strings.HasPrefix(key, "sk-") {
			t.Fatalf("expected sk-prefixed key, got %q", key)
		}
		if firstKey == "" {
			firstKey = key
			continue
		}
		if key != firstKey {
			t.Fatalf("expected concurrent requests to return the same key, got %q and %q", firstKey, key)
		}
	}
	if count := countLeapcoreMachineTokens(t, db, user.Id); count != 1 {
		t.Fatalf("expected concurrent create to produce 1 token, got %d", count)
	}
}

func TestCreateLeapCoreMachineUserCreatesProvisionedUser(t *testing.T) {
	db := setupLeapcoreControllerTestDB(t)
	machineID := leapcoreMachineID("provision-machine")

	ctx, recorder := newLeapcoreMachineUserContext(t, machineID)
	CreateLeapCoreMachineUser(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", recorder.Code, recorder.Body.String())
	}
	data := decodeLeapcoreMachineUserResponse(t, recorder)
	if !data.Created {
		t.Fatalf("expected machine user to be created")
	}
	if data.MachineID != machineID {
		t.Fatalf("expected normalized machine_id %q, got %q", machineID, data.MachineID)
	}
	if data.Username != deriveLeapcoreUsername(machineID) {
		t.Fatalf("expected username %q, got %q", deriveLeapcoreUsername(machineID), data.Username)
	}
	if data.Password != deriveLeapcorePassword(machineID) {
		t.Fatalf("expected derived password %q, got %q", deriveLeapcorePassword(machineID), data.Password)
	}

	var user model.User
	if err := db.First(&user, "username = ?", data.Username).Error; err != nil {
		t.Fatalf("failed to fetch created machine user: %v", err)
	}
	if user.Remark != machineID {
		t.Fatalf("expected remark %q, got %q", machineID, user.Remark)
	}
	if user.Role != common.RoleCommonUser || user.Status != common.UserStatusEnabled {
		t.Fatalf("created machine user has unexpected role/status: %#v", user)
	}
	if common.ValidatePasswordAndHash(data.Password, user.Password) {
		return
	}
	t.Fatalf("created machine user password does not match derived password")
}

func TestCreateLeapCoreMachineUserAcceptsBase64MachineID(t *testing.T) {
	machineID := leapcoreMachineID("base64-provision-machine")
	encodedMachineID := base64.StdEncoding.EncodeToString([]byte(machineID))
	setupLeapcoreControllerTestDB(t)

	ctx, recorder := newLeapcoreMachineUserContext(t, encodedMachineID)
	CreateLeapCoreMachineUser(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", recorder.Code, recorder.Body.String())
	}
	data := decodeLeapcoreMachineUserResponse(t, recorder)
	if data.MachineID != machineID {
		t.Fatalf("expected decoded machine_id %q, got %q", machineID, data.MachineID)
	}
	if data.Username != deriveLeapcoreUsername(machineID) || data.Password != deriveLeapcorePassword(machineID) {
		t.Fatalf("unexpected derived credentials: %#v", data)
	}
}

func TestCreateLeapCoreMachineUserAcceptsRawFingerprint(t *testing.T) {
	rawFingerprint := "4c4c45444d0032108058c4c04f4d5332/CN1296378H00AB"
	machineID := leapcoreMachineID(rawFingerprint)
	setupLeapcoreControllerTestDB(t)

	ctx, recorder := newLeapcoreMachineUserContext(t, rawFingerprint)
	CreateLeapCoreMachineUser(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", recorder.Code, recorder.Body.String())
	}
	data := decodeLeapcoreMachineUserResponse(t, recorder)
	if data.MachineID != machineID {
		t.Fatalf("expected normalized machine_id %q, got %q", machineID, data.MachineID)
	}
	if data.Username != deriveLeapcoreUsername(machineID) || data.Password != deriveLeapcorePassword(machineID) {
		t.Fatalf("unexpected derived credentials: %#v", data)
	}
}

func TestCreateLeapCoreMachineUserIsIdempotent(t *testing.T) {
	db := setupLeapcoreControllerTestDB(t)
	machineID := leapcoreMachineID("idempotent-provision-machine")

	ctx, recorder := newLeapcoreMachineUserContext(t, machineID)
	CreateLeapCoreMachineUser(ctx)
	first := decodeLeapcoreMachineUserResponse(t, recorder)

	ctx, recorder = newLeapcoreMachineUserContext(t, machineID)
	CreateLeapCoreMachineUser(ctx)
	second := decodeLeapcoreMachineUserResponse(t, recorder)

	if !first.Created || second.Created {
		t.Fatalf("expected first request created=true and second created=false, got %#v then %#v", first, second)
	}
	if first.ID != second.ID || first.Username != second.Username || first.Password != second.Password {
		t.Fatalf("expected idempotent machine user response, got %#v then %#v", first, second)
	}

	var count int64
	if err := db.Model(&model.User{}).Where("username = ?", deriveLeapcoreUsername(machineID)).Count(&count).Error; err != nil {
		t.Fatalf("failed to count machine users: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one machine user, got %d", count)
	}
}

func TestCreateLeapCoreMachineUserRejectsRemarkConflict(t *testing.T) {
	db := setupLeapcoreControllerTestDB(t)
	machineID := leapcoreMachineID("remark-conflict-provision-machine")
	seedLeapcoreMachineUser(t, db, machineID, func(user *model.User) {
		user.Remark = leapcoreMachineID("other-machine")
	})

	ctx, recorder := newLeapcoreMachineUserContext(t, machineID)
	CreateLeapCoreMachineUser(ctx)

	assertLeapcoreError(t, recorder, http.StatusOK)
}
