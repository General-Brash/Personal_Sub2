package service

import "testing"

func TestOpenAIStreamErrorEventShouldFailover(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		message  string
		wantFail bool
	}{
		{
			name:     "rate limit error event",
			payload:  `{"type":"error","error":{"type":"rate_limit_error","message":"try again later"}}`,
			message:  "try again later",
			wantFail: true,
		},
		{
			name:     "invalid request is not account failure",
			payload:  `{"type":"error","error":{"type":"invalid_request_error","message":"invalid input"}}`,
			message:  "invalid input",
			wantFail: false,
		},
		{
			name:     "context window is not account failure",
			payload:  `{"type":"error","error":{"message":"context window exceeded"}}`,
			message:  "context window exceeded",
			wantFail: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := openAIStreamErrorEventShouldFailover([]byte(tt.payload), tt.message); got != tt.wantFail {
				t.Fatalf("openAIStreamErrorEventShouldFailover() = %v, want %v", got, tt.wantFail)
			}
		})
	}
}
