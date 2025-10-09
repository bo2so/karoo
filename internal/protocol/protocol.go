package protocol

type JSONMsg struct {
	ID     *int64      `json:"id,omitempty"`
	Method string      `json:"method,omitempty"`
	Params interface{} `json:"params,omitempty"`
	Result interface{} `json:"result,omitempty"`
	Error  interface{} `json:"error,omitempty"`
}
