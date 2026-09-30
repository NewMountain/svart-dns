//go:build !linux

package svart

import (
	"errors"
	"os/exec"
)

func limitInvestigateWorker() error {
	return errors.New("investigation requires Linux resource isolation")
}
func configureInvestigateProcess(cmd *exec.Cmd) {}
