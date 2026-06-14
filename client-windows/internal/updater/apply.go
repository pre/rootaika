package updater

import (
	"flag"
	"fmt"
)

// ApplyArgs are the parameters the detached apply-update helper needs to swap
// the on-disk binary and restart the service.
type ApplyArgs struct {
	// Target is the live exe to replace (the installed rootaika.exe).
	Target string
	// Staged is the verified new exe to copy over Target.
	Staged string
	// Service is the Windows service name to stop before and start after the swap.
	Service string
	// AgentProcess is the agent image name to kill before the swap so the file is
	// not in use.
	AgentProcess string
}

// ParseApplyArgs parses the apply-update subcommand flags from the argument list
// (os.Args[2:]).
func ParseApplyArgs(args []string) (ApplyArgs, error) {
	fs := flag.NewFlagSet("apply-update", flag.ContinueOnError)
	var a ApplyArgs
	fs.StringVar(&a.Target, "target", "", "path to the live rootaika.exe to replace")
	fs.StringVar(&a.Staged, "staged", "", "path to the verified staged exe to install")
	fs.StringVar(&a.Service, "service", "rootaika-service", "Windows service name to restart")
	fs.StringVar(&a.AgentProcess, "agent-process", "rootaika.exe", "agent image name to terminate before swap")
	if err := fs.Parse(args); err != nil {
		return ApplyArgs{}, err
	}
	if a.Target == "" || a.Staged == "" {
		return ApplyArgs{}, fmt.Errorf("apply-update requires -target and -staged")
	}
	return a, nil
}
