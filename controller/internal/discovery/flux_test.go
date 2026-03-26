package discovery

import (
	"testing"
)

func TestExtractECRRepoName(t *testing.T) {
	tests := []struct {
		name    string
		image   string
		want    string
		wantErr bool
	}{
		{
			name:  "simple repo",
			image: "123456789012.dkr.ecr.us-east-1.amazonaws.com/my-repo",
			want:  "my-repo",
		},
		{
			name:  "nested repo path",
			image: "123456789012.dkr.ecr.us-east-1.amazonaws.com/team/my-repo",
			want:  "team/my-repo",
		},
		{
			name:  "with tag",
			image: "123456789012.dkr.ecr.us-east-1.amazonaws.com/my-repo:latest",
			want:  "my-repo",
		},
		{
			name:  "with digest",
			image: "123456789012.dkr.ecr.us-east-1.amazonaws.com/my-repo@sha256:abc123",
			want:  "my-repo",
		},
		{
			name:  "with tag and digest",
			image: "123456789012.dkr.ecr.us-east-1.amazonaws.com/my-repo:v1@sha256:abc123",
			want:  "my-repo",
		},
		{
			name:  "deeply nested path",
			image: "123456789012.dkr.ecr.eu-west-1.amazonaws.com/org/team/service",
			want:  "org/team/service",
		},
		{
			name:    "not ECR image",
			image:   "docker.io/library/nginx:latest",
			wantErr: true,
		},
		{
			name:    "missing repo path",
			image:   "123456789012.dkr.ecr.us-east-1.amazonaws.com/",
			wantErr: true,
		},
		{
			name:    "no slash",
			image:   "123456789012.dkr.ecr.us-east-1.amazonaws.com",
			wantErr: true,
		},
		{
			name:    "empty string",
			image:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractECRRepoName(tt.image)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractECRRepoName(%q) error = %v, wantErr %v", tt.image, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("extractECRRepoName(%q) = %q, want %q", tt.image, got, tt.want)
			}
		})
	}
}

func TestExtractRegex(t *testing.T) {
	// Import the type inline via helper struct since we can't easily construct
	// full ImagePolicy objects without the scheme. Test the exported function
	// through the package-level extractRegex.

	// extractRegex with nil slice returns empty
	if got := extractRegex(nil); got != "" {
		t.Errorf("extractRegex(nil) = %q, want empty", got)
	}
}
