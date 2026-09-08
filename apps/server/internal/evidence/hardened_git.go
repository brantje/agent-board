package evidence

import (
	"os"
	"strings"
)

func hardenedGitConfig() []string {
	return []string{
		"-c", "safe.directory=*",
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.fsmonitor=false",
		"-c", "credential.helper=",
		"-c", "diff.external=",
	}
}

func hardenedGitEnv() []string {
	env := make([]string, 0, len(os.Environ())+8)
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		upper := strings.ToUpper(key)
		if strings.HasPrefix(upper, "GIT_") || strings.HasPrefix(upper, "SSH_") || upper == "HOME" || upper == "PAGER" || upper == "XDG_CONFIG_HOME" {
			continue
		}
		env = append(env, item)
	}
	return append(env,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
		"GIT_SSH_COMMAND=false",
		"GIT_PAGER=cat",
		"PAGER=cat",
	)
}
