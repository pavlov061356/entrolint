package cli

import "testing"

func TestCheckCmd_FlagsRegistered(t *testing.T) {
	for _, name := range []string{"base", "head", "format", "json", "config", "recalibrate", "root"} {
		if checkCmd.Flags().Lookup(name) == nil {
			t.Errorf("flag --%s not registered", name)
		}
	}
}
