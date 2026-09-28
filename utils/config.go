package utils

type GlobalConfig struct {
	ScriptsFile      string
	ClientsFile      string
	Binder           string
	SpeedLimit       uint64
	PauseSecond      uint
	ConnTaskTreading uint
	TLS              bool
	TLSCertFile      string
	TLSKeyFile       string
	NoSpeedFlag      bool
	MaxmindDB        string
}

var GCFG GlobalConfig
