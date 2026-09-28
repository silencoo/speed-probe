package service

import (
	"github.com/silencoo/speed-probe/auth"
	"github.com/silencoo/speed-probe/utils"
)

func ValidateAuthConfig() error {
	if _, err := auth.Load(utils.GCFG.ClientsFile); err != nil {
		return err
	}
	_, err := LoadScripts(utils.GCFG.ScriptsFile)
	return err
}
