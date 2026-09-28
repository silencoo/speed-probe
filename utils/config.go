package utils

type GlobalConfig struct {
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
