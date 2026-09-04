package http

import (
	"encoding/json"
	"net/http"
)

// APIErrorDetail 封装类型化错误的具体错误字段。
type APIErrorDetail struct {
	Code      string      `json:"code"`
	Message   string      `json:"message"`
	RequestID string      `json:"requestId,omitempty"`
	Details   interface{} `json:"details,omitempty"`
}

// APIErrorResponse 统一的 API 错误响应格式。
type APIErrorResponse struct {
	Error APIErrorDetail `json:"error"`
}

// WriteJSONError 向响应写入统一 schema 的 JSON 错误。
func WriteJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(APIErrorResponse{
		Error: APIErrorDetail{
			Code:    code,
			Message: message,
		},
	})
}

// MethodNotAllowedHandler 返回 405 Method Not Allowed 并设置 Allow 头。
func MethodNotAllowed(w http.ResponseWriter, allowedMethods string) {
	w.Header().Set("Allow", allowedMethods)
	WriteJSONError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
}
