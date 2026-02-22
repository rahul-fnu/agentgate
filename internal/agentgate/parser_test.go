package agentgate

import (
	"testing"
)

func TestParseKubectl(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantAction string
		wantType   string
		wantRes    string
		wantResN   string
		wantNS     string
		wantStatus ParseStatus
	}{
		{"delete namespace", []string{"delete", "namespace", "foo"}, "delete", "destructive", "namespace", "foo", "", ParseStatusParsed},
		{"get pods", []string{"get", "pods"}, "get", "read", "pods", "", "", ParseStatusParsed},
		{"apply with namespace", []string{"apply", "-f", "x.yaml", "-n", "prod"}, "apply", "write", "", "", "prod", ParseStatusPartial},
		{"scale to zero", []string{"scale", "deployment", "web", "--replicas=0"}, "scale", "destructive", "deployment", "web", "", ParseStatusParsed},
		{"scale to 3", []string{"scale", "deployment", "web", "--replicas=3"}, "scale", "other", "deployment", "web", "", ParseStatusParsed},
		{"empty args", []string{}, "", "other", "", "", "", ParseStatusUnknown},
		{"describe pod", []string{"describe", "pod", "mypod"}, "describe", "read", "pod", "mypod", "", ParseStatusParsed},
		{"drain node", []string{"drain", "node1"}, "drain", "destructive", "node1", "", "", ParseStatusParsed},
		{"logs", []string{"logs", "pod1"}, "logs", "read", "pod1", "", "", ParseStatusParsed},
		{"action only no resource", []string{"delete"}, "delete", "destructive", "", "", "", ParseStatusPartial},
		{"namespace flag equals", []string{"get", "pods", "--namespace=kube-system"}, "get", "read", "pods", "", "kube-system", ParseStatusParsed},
		{"scale replicas separate arg", []string{"scale", "deploy", "web", "--replicas", "0"}, "scale", "destructive", "deploy", "web", "", ParseStatusParsed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ParseCommand("kubectl", tt.args, "/tmp", false)
			if ctx.Action != tt.wantAction {
				t.Errorf("action = %q, want %q", ctx.Action, tt.wantAction)
			}
			if ctx.ActionType != tt.wantType {
				t.Errorf("action_type = %q, want %q", ctx.ActionType, tt.wantType)
			}
			if ctx.Resource != tt.wantRes {
				t.Errorf("resource = %q, want %q", ctx.Resource, tt.wantRes)
			}
			if ctx.ResourceName != tt.wantResN {
				t.Errorf("resource_name = %q, want %q", ctx.ResourceName, tt.wantResN)
			}
			if ctx.Namespace != tt.wantNS {
				t.Errorf("namespace = %q, want %q", ctx.Namespace, tt.wantNS)
			}
			if ctx.ParseStatus != tt.wantStatus {
				t.Errorf("parse_status = %q, want %q", ctx.ParseStatus, tt.wantStatus)
			}
		})
	}
}

func TestParseTerraform(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantAction string
		wantType   string
		wantStatus ParseStatus
	}{
		{"apply", []string{"apply"}, "apply", "write", ParseStatusParsed},
		{"destroy", []string{"destroy"}, "destroy", "destructive", ParseStatusParsed},
		{"plan", []string{"plan"}, "plan", "other", ParseStatusParsed},
		{"state rm", []string{"state", "rm", "aws_instance.web"}, "state-rm", "destructive", ParseStatusParsed},
		{"state mv", []string{"state", "mv", "a", "b"}, "state-mv", "destructive", ParseStatusParsed},
		{"force-unlock", []string{"force-unlock", "abc123"}, "force-unlock", "destructive", ParseStatusParsed},
		{"workspace new", []string{"workspace", "new", "staging"}, "workspace-new", "other", ParseStatusParsed},
		{"chdir", []string{"-chdir=/opt/tf", "apply"}, "apply", "write", ParseStatusParsed},
		{"empty", []string{}, "", "other", ParseStatusUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ParseCommand("terraform", tt.args, "/tmp", false)
			if ctx.Action != tt.wantAction {
				t.Errorf("action = %q, want %q", ctx.Action, tt.wantAction)
			}
			if ctx.ActionType != tt.wantType {
				t.Errorf("action_type = %q, want %q", ctx.ActionType, tt.wantType)
			}
			if ctx.ParseStatus != tt.wantStatus {
				t.Errorf("parse_status = %q, want %q", ctx.ParseStatus, tt.wantStatus)
			}
		})
	}
}

func TestParseTerraformChdir(t *testing.T) {
	ctx := ParseCommand("terraform", []string{"-chdir=infra/prod", "apply"}, "/home/user", false)
	if ctx.WorkingDir != "/home/user/infra/prod" {
		t.Errorf("working_dir = %q, want /home/user/infra/prod", ctx.WorkingDir)
	}

	ctx2 := ParseCommand("terraform", []string{"-chdir=/opt/tf", "plan"}, "/home/user", false)
	if ctx2.WorkingDir != "/opt/tf" {
		t.Errorf("working_dir = %q, want /opt/tf", ctx2.WorkingDir)
	}
}

func TestParseHelm(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantAction string
		wantType   string
		wantResN   string
	}{
		{"install", []string{"install", "myrelease", "mychart"}, "install", "write", "myrelease"},
		{"uninstall", []string{"uninstall", "myrelease"}, "uninstall", "destructive", "myrelease"},
		{"upgrade", []string{"upgrade", "myrelease", "mychart"}, "upgrade", "write", "myrelease"},
		{"list", []string{"list"}, "list", "read", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ParseCommand("helm", tt.args, "/tmp", false)
			if ctx.Action != tt.wantAction {
				t.Errorf("action = %q, want %q", ctx.Action, tt.wantAction)
			}
			if ctx.ActionType != tt.wantType {
				t.Errorf("action_type = %q, want %q", ctx.ActionType, tt.wantType)
			}
			if ctx.ResourceName != tt.wantResN {
				t.Errorf("resource_name = %q, want %q", ctx.ResourceName, tt.wantResN)
			}
		})
	}
}

func TestParseAWS(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantRes    string
		wantAction string
		wantType   string
		wantStatus ParseStatus
	}{
		{"ec2 terminate", []string{"ec2", "terminate-instances", "--instance-ids", "i-123"}, "ec2", "terminate-instances", "destructive", ParseStatusParsed},
		{"s3 ls", []string{"s3", "ls"}, "s3", "ls", "other", ParseStatusParsed},
		{"rds delete", []string{"rds", "delete-db-instance", "--db-instance-identifier", "mydb"}, "rds", "delete-db-instance", "destructive", ParseStatusParsed},
		{"single arg", []string{"s3"}, "", "s3", "other", ParseStatusPartial},
		{"empty", []string{}, "", "", "other", ParseStatusPartial},
		{"with profile", []string{"--profile", "prod", "ec2", "describe-instances"}, "ec2", "describe-instances", "read", ParseStatusParsed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ParseCommand("aws", tt.args, "/tmp", false)
			if ctx.Resource != tt.wantRes {
				t.Errorf("resource = %q, want %q", ctx.Resource, tt.wantRes)
			}
			if ctx.Action != tt.wantAction {
				t.Errorf("action = %q, want %q", ctx.Action, tt.wantAction)
			}
			if ctx.ActionType != tt.wantType {
				t.Errorf("action_type = %q, want %q", ctx.ActionType, tt.wantType)
			}
			if ctx.ParseStatus != tt.wantStatus {
				t.Errorf("parse_status = %q, want %q", ctx.ParseStatus, tt.wantStatus)
			}
		})
	}
}

func TestParseGCloud(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantRes    string
		wantAction string
		wantType   string
		wantStatus ParseStatus
	}{
		{"compute instances create", []string{"compute", "instances", "create", "vm-1"}, "compute/instances", "create", "write", ParseStatusParsed},
		{"projects delete", []string{"projects", "delete", "my-project"}, "projects", "delete", "destructive", ParseStatusParsed},
		{"container clusters delete", []string{"container", "clusters", "delete", "mycluster"}, "container/clusters", "delete", "destructive", ParseStatusParsed},
		{"single arg", []string{"compute"}, "compute", "compute", "other", ParseStatusPartial},
		{"empty", []string{}, "", "", "other", ParseStatusUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ParseCommand("gcloud", tt.args, "/tmp", false)
			if ctx.Resource != tt.wantRes {
				t.Errorf("resource = %q, want %q", ctx.Resource, tt.wantRes)
			}
			if ctx.Action != tt.wantAction {
				t.Errorf("action = %q, want %q", ctx.Action, tt.wantAction)
			}
			if ctx.ActionType != tt.wantType {
				t.Errorf("action_type = %q, want %q", ctx.ActionType, tt.wantType)
			}
			if ctx.ParseStatus != tt.wantStatus {
				t.Errorf("parse_status = %q, want %q", ctx.ParseStatus, tt.wantStatus)
			}
		})
	}
}

func TestParseGit(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantAction string
		wantType   string
		wantRes    string
		wantResN   string
	}{
		{"push force", []string{"push", "--force", "origin", "main"}, "push-force", "destructive", "origin", "main"},
		{"push force-with-lease", []string{"push", "--force-with-lease", "origin", "feat"}, "push-force", "destructive", "origin", "feat"},
		{"push normal", []string{"push", "origin", "main"}, "push", "other", "origin", "main"},
		{"reset hard", []string{"reset", "--hard", "HEAD~1"}, "reset-hard", "destructive", "head~1", ""},
		{"reset soft", []string{"reset", "--soft", "HEAD~1"}, "reset", "other", "head~1", ""},
		{"clean force", []string{"clean", "-f", "-d"}, "clean-force", "destructive", "", ""},
		{"commit", []string{"commit", "-m", "fix bug"}, "commit", "write", "fix bug", ""},
		{"log", []string{"log", "--oneline"}, "log", "read", "", ""},
		{"status", []string{"status"}, "status", "read", "", ""},
		{"push -f shorthand", []string{"push", "-f", "origin", "main"}, "push-force", "destructive", "main", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ParseCommand("git", tt.args, "/tmp", false)
			if ctx.Action != tt.wantAction {
				t.Errorf("action = %q, want %q", ctx.Action, tt.wantAction)
			}
			if ctx.ActionType != tt.wantType {
				t.Errorf("action_type = %q, want %q", ctx.ActionType, tt.wantType)
			}
			if ctx.Resource != tt.wantRes {
				t.Errorf("resource = %q, want %q", ctx.Resource, tt.wantRes)
			}
			if ctx.ResourceName != tt.wantResN {
				t.Errorf("resource_name = %q, want %q", ctx.ResourceName, tt.wantResN)
			}
		})
	}
}

func TestParseDocker(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantAction string
		wantType   string
		wantRes    string
	}{
		{"system prune", []string{"system", "prune"}, "system-prune", "destructive", "system"},
		{"compose down", []string{"compose", "down"}, "compose-down", "destructive", "compose"},
		{"rm force", []string{"rm", "-f", "container1"}, "rm-force", "destructive", "rm"},
		{"run", []string{"run", "nginx"}, "run", "other", "run"},
		{"ps", []string{"ps"}, "ps", "other", "ps"},
		{"compose up", []string{"compose", "up", "-d"}, "compose-up", "write", "compose"},
		{"empty", []string{}, "", "other", "", },
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ParseCommand("docker", tt.args, "/tmp", false)
			if ctx.Action != tt.wantAction {
				t.Errorf("action = %q, want %q", ctx.Action, tt.wantAction)
			}
			if ctx.ActionType != tt.wantType {
				t.Errorf("action_type = %q, want %q", ctx.ActionType, tt.wantType)
			}
			if ctx.Resource != tt.wantRes {
				t.Errorf("resource = %q, want %q", ctx.Resource, tt.wantRes)
			}
		})
	}
}

func TestClassifyActionType(t *testing.T) {
	tests := []struct {
		tool   string
		action string
		want   string
	}{
		{"kubectl", "get", "read"},
		{"kubectl", "describe", "read"},
		{"kubectl", "delete", "destructive"},
		{"kubectl", "apply", "write"},
		{"kubectl", "drain", "destructive"},
		{"terraform", "destroy", "destructive"},
		{"terraform", "apply", "write"},
		{"terraform", "plan", "other"},
		{"terraform", "state-rm", "destructive"},
		{"terraform", "force-unlock", "destructive"},
		{"helm", "install", "write"},
		{"helm", "uninstall", "destructive"},
		{"git", "push-force", "destructive"},
		{"git", "reset-hard", "destructive"},
		{"git", "commit", "write"},
		{"git", "log", "read"},
		{"git", "diff", "read"},
		{"docker", "system-prune", "destructive"},
		{"docker", "compose-down", "destructive"},
		{"docker", "rm-force", "destructive"},
		{"docker", "compose-up", "write"},
		{"aws", "describe-instances", "read"},
		{"aws", "terminate-instances", "destructive"},
		{"aws", "create-bucket", "write"},
		{"gcloud", "delete", "destructive"},
		{"gcloud", "describe-instances", "read"},
		{"kubectl", "version", "read"},
		{"kubectl", "options", "read"},
	}
	for _, tt := range tests {
		t.Run(tt.tool+"/"+tt.action, func(t *testing.T) {
			got := classifyActionType(tt.tool, tt.action)
			if got != tt.want {
				t.Errorf("classifyActionType(%q, %q) = %q, want %q", tt.tool, tt.action, got, tt.want)
			}
		})
	}
}

func TestFirstPositional(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantVal string
		wantIdx int
	}{
		{"simple", []string{"get", "pods"}, "get", 0},
		{"with flags", []string{"-n", "prod", "get", "pods"}, "get", 2},
		{"empty", []string{}, "", -1},
		{"only flags", []string{"-n", "prod"}, "", -1},
		{"flag with context", []string{"--context", "my-ctx", "apply"}, "apply", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, idx := firstPositional(tt.args)
			if val != tt.wantVal {
				t.Errorf("val = %q, want %q", val, tt.wantVal)
			}
			if idx != tt.wantIdx {
				t.Errorf("idx = %d, want %d", idx, tt.wantIdx)
			}
		})
	}
}

func TestCollectPositionals(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"simple", []string{"get", "pods"}, []string{"get", "pods"}},
		{"with flags", []string{"-n", "prod", "get", "pods"}, []string{"get", "pods"}},
		{"long flags", []string{"--namespace", "prod", "get"}, []string{"get"}},
		{"equals flag", []string{"--namespace=prod", "get"}, []string{"get"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collectPositionals(tt.args)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d: %v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestLooksLikeScaleToZero(t *testing.T) {
	if !looksLikeScaleToZero([]string{"--replicas=0"}) {
		t.Error("should detect --replicas=0")
	}
	if !looksLikeScaleToZero([]string{"--replicas", "0"}) {
		t.Error("should detect --replicas 0")
	}
	if looksLikeScaleToZero([]string{"--replicas=3"}) {
		t.Error("should not detect --replicas=3")
	}
	if looksLikeScaleToZero([]string{"--replicas", "5"}) {
		t.Error("should not detect --replicas 5")
	}
}

func TestLooksLikeActionToken(t *testing.T) {
	for _, tok := range []string{"create", "delete", "update", "describe", "list", "terminate", "deploy", "run", "start", "stop"} {
		if !looksLikeActionToken(tok) {
			t.Errorf("should match %q", tok)
		}
	}
	for _, tok := range []string{"compute", "instances", "container", "clusters", "projects"} {
		if looksLikeActionToken(tok) {
			t.Errorf("should not match %q", tok)
		}
	}
}

func TestConsumesNextValueFlag(t *testing.T) {
	for _, f := range []string{"namespace", "profile", "project", "context", "cluster", "zone", "region"} {
		if !consumesNextValueFlag(f) {
			t.Errorf("should consume %q", f)
		}
	}
	if consumesNextValueFlag("verbose") {
		t.Error("should not consume verbose")
	}
}

func TestHasFlag(t *testing.T) {
	args := []string{"push", "--force", "origin", "main"}
	if !hasFlag(args, "--force") {
		t.Error("should find --force")
	}
	if hasFlag(args, "--dry-run") {
		t.Error("should not find --dry-run")
	}
	argsEq := []string{"push", "--force=true", "origin"}
	if !hasFlag(argsEq, "--force") {
		t.Error("should find --force= prefix")
	}
}

func TestParseUnknownTool(t *testing.T) {
	ctx := ParseCommand("unknown-tool", []string{"something"}, "/tmp", false)
	if ctx.ParseStatus != ParseStatusUnknown {
		t.Errorf("unknown tool should have ParseStatusUnknown, got %q", ctx.ParseStatus)
	}
	if ctx.ActionType != "other" {
		t.Errorf("unknown tool should have action_type other, got %q", ctx.ActionType)
	}
}

func TestRawCommandBuilt(t *testing.T) {
	ctx := ParseCommand("kubectl", []string{"delete", "namespace", "prod"}, "/tmp", false)
	if ctx.RawCommand != "kubectl delete namespace prod" {
		t.Errorf("raw_command = %q", ctx.RawCommand)
	}
}
