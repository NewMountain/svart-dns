package svart

// Test-only loose envelope supports legacy characterization assertions; the
// production writer is generic and every wire payload has a concrete type.
type apiResponse struct {
	Data      interface{} `json:"data"`
	Error     *string     `json:"error"`
	ErrorCode string      `json:"error_code,omitempty"`
}
