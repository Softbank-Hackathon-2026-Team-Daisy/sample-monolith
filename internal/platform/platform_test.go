package platform

import "testing"

func TestDetect(t *testing.T) {
	tests := []struct {
		name   string
		env    map[string]string
		docker bool
		want   string
	}{
		{"override wins", map[string]string{"DEPLOY_PLATFORM": "Demo VM", "AWS_EXECUTION_ENV": "AWS_ECS_FARGATE"}, true, "Demo VM"},
		{"blank override ignored", map[string]string{"DEPLOY_PLATFORM": "  "}, false, "Local"},
		{"ecs fargate", map[string]string{"AWS_EXECUTION_ENV": "AWS_ECS_FARGATE"}, false, "AWS · ECS Fargate"},
		{"ecs on ec2", map[string]string{"AWS_EXECUTION_ENV": "AWS_ECS_EC2"}, true, "AWS · ECS"},
		{"cloud run", map[string]string{"K_SERVICE": "hellocalc"}, false, "GCP · Cloud Run"},
		{"docker", nil, true, "On-premises · Docker"},
		{"local", nil, false, "Local"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(k string) string { return tt.env[k] }
			exists := func(p string) bool { return tt.docker && p == "/.dockerenv" }
			if got := Detect(getenv, exists); got != tt.want {
				t.Errorf("Detect() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFileExists(t *testing.T) {
	if !FileExists(t.TempDir()) {
		t.Error("FileExists(temp dir) = false, want true")
	}
	if FileExists(t.TempDir() + "/missing") {
		t.Error("FileExists(missing) = true, want false")
	}
}
