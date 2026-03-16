package checks

// Status represents the outcome level of a check.
type Status string

const (
	StatusOK       Status = "OK"
	StatusWarning  Status = "WARNING"
	StatusCritical Status = "CRITICAL"
	StatusError    Status = "ERROR"
)

// Result holds the outcome of a single check.
type Result struct {
	Name    string      `json:"name"`
	Status  Status      `json:"status"`
	Message string      `json:"message"`
	Value   interface{} `json:"value,omitempty"`
}

// Check is the interface every check must implement.
type Check interface {
	Name() string
	Run() Result
}
