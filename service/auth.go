package service

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/silencoo/speed-probe/auth"
	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/utils"
)

func ValidateAuthConfig() error {
	_, err := auth.Load(utils.GCFG.ClientsFile)
	return err
}

func authenticate(data []byte, manager *auth.Manager) (*interfaces.SlaveRequest, *auth.Client, error) {
	var e auth.Envelope
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, nil, errors.New("invalid authentication envelope")
	}
	client, err := manager.Verify(e, time.Now())
	if err != nil {
		return nil, nil, err
	}
	var req interfaces.SlaveRequest
	if err = json.Unmarshal([]byte(e.Payload), &req); err != nil {
		return nil, nil, errors.New("invalid request payload")
	}
	if err = client.Allows(&req); err != nil {
		return nil, nil, err
	}
	return &req, &client, nil
}
