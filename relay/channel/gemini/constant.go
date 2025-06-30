package gemini

var ModelList = []string{
	// preview series
	"gemini-2.0-flash-preview-image-generation",
	"gemini-2.5-flash-lite-preview-06-17",
	"gemini-2.5-flash-preview-04-17",
	"gemini-2.5-flash-preview-05-20",
	"gemini-2.5-pro-preview-06-05",
	// gemini 2.5 Series
	"gemini-2.5-pro",
	"gemini-2.5-flash",
	// gemini 2.0 Series
	"gemini-2.0-flash",
	"gemini-2.0-flash-lite",
	// imagen models
	"imagen-3.0-generate-002",
	// embedding models
	"gemini-embedding-exp-03-07",
	"text-embedding-004",
	"embedding-001",
}

var SafetySettingList = []string{
	"HARM_CATEGORY_HARASSMENT",
	"HARM_CATEGORY_HATE_SPEECH",
	"HARM_CATEGORY_SEXUALLY_EXPLICIT",
	"HARM_CATEGORY_DANGEROUS_CONTENT",
	"HARM_CATEGORY_CIVIC_INTEGRITY",
}

var ChannelName = "google gemini"
