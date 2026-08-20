package config

import "github.com/peergo/peergo/contracts/go/deploymentv1"

func isSingleServerDeployment() (bool, error) {
	mode, err := deploymentv1.Load()
	if err != nil {
		return false, err
	}
	return mode == deploymentv1.SingleServer, nil
}
