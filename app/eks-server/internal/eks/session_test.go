package eks

import "testing"

func TestParseMarkedOutput(t *testing.T) {
	marker := "__OCULUS_EKS_TEST__"
	raw := marker + "_BEGIN\r\npod-a Running\r\npod-b Running\r\n" + marker + "_END:0\r\n" + marker + "_DONE\r\nprompt# "

	result, err := parseMarkedOutput(raw, marker)
	if err != nil {
		t.Fatalf("parseMarkedOutput() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}
	want := "pod-a Running\npod-b Running"
	if result.Output != want {
		t.Fatalf("Output = %q, want %q", result.Output, want)
	}
}
