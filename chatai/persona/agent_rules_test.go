package persona

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentRulesRuntimeEnvironment(t *testing.T) {
	rules := AgentRules()
	require.Contains(t, rules, runtime.GOOS)
	require.Contains(t, rules, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		require.Contains(t, rules, "powershell.exe")
	} else {
		require.Contains(t, rules, "/bin/sh -c")
	}
}
