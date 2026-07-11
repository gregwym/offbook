package diskspace

import "testing"

// TestParseDfOutput_Scenarios covers macOS and Linux df -Pk output shapes,
// including the long-device-name line-wrap case, and malformed input.
func TestParseDfOutput_Scenarios(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		want    float64
		wantErr bool
	}{
		{
			name: "macOS df -Pk",
			output: "Filesystem     1024-blocks      Used  Available Capacity Mounted on\n" +
				"/dev/disk3s1s1   974716400  10475840  600038360      2%    /\n",
			want: 98,
		},
		{
			name: "linux df -Pk",
			output: "Filesystem     1024-blocks    Used Available Capacity Mounted on\n" +
				"/dev/vda1          20511356 8628936  10604140      45% /\n",
			want: 55,
		},
		{
			name: "near full disk",
			output: "Filesystem     1024-blocks    Used Available Capacity Mounted on\n" +
				"/dev/vda1          20511356 19486788   0         100% /\n",
			want: 0,
		},
		{
			name: "trailing blank line is ignored",
			output: "Filesystem     1024-blocks    Used Available Capacity Mounted on\n" +
				"/dev/vda1          20511356 8628936  10604140      45% /\n\n",
			want: 55,
		},
		{
			name:    "empty output",
			output:  "",
			wantErr: true,
		},
		{
			name:    "header only, no data line",
			output:  "Filesystem     1024-blocks    Used Available Capacity Mounted on\n",
			wantErr: true,
		},
		{
			name:    "too few fields on data line",
			output:  "Filesystem 1024-blocks\n/dev/vda1  20511356\n",
			wantErr: true,
		},
		{
			name:    "non-numeric capacity field",
			output:  "Filesystem 1024-blocks Used Available Capacity Mounted-on\n/dev/vda1 20511356 8628936 10604140 abc% /\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDfOutput(tt.output)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseDfOutput(%q) = %v, nil; want error", tt.output, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDfOutput(%q) unexpected error: %v", tt.output, err)
			}
			if got != tt.want {
				t.Errorf("parseDfOutput(%q) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}
