package interfaces

type ScriptType string

const (
	STypeMedia ScriptType = "media"
	STypeIP    ScriptType = "ip"
)

type Script struct {
	ID            string     `yaml:"-" fw:"readonly"`
	Type          ScriptType `yaml:"type"`
	Content       string     `yaml:"content"`
	TimeoutMillis uint64     `yaml:"timeout,omitempty"`
}

type ScriptResult struct {
	Text        string
	Status      string `json:",omitempty"`
	Color       string
	Background  string
	TimeElapsed int64
}

func (sr *ScriptResult) Clone() *ScriptResult {
	return &ScriptResult{
		Text:        sr.Text,
		Status:      sr.Status,
		Color:       sr.Color,
		Background:  sr.Background,
		TimeElapsed: sr.TimeElapsed,
	}
}
