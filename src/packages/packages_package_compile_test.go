package packages

import (
	"errors"
	"reflect"
	"testing"
)

func TestPackageCompile(t *testing.T) {
	table := map[string]struct {
		input     InputPackage
		output    Package
		responses Responses
		err       bool
	}{
		"basic": {
			input: InputPackage{
				Image:       "debian:latest",
				Environment: map[string]string{"HELLO": "scarlett"},
				Network:     InputPackageNetwork{External: map[string]string{"80": "80"}, Internal: map[string]string{"128": "128"}},
				Volumes:     map[string]InputPackageVolume{},
				Questions:   map[string]Question{},
			},
			output: Package{
				Image:       "docker.io/library/debian:latest",
				Environment: map[string]string{"HELLO": "scarlett"},
				Network:     PackageNetwork{External: PortMap{80: 80}, Internal: PortMap{128: 128}},
				Volumes:     map[string]PackageVolume{},
			},
			responses: Responses{},
			err:       false,
		},
		"basic-template": {
			input: InputPackage{
				Image:       "debian:latest",
				Environment: map[string]string{"HELLO": "@name@"},
				Network:     InputPackageNetwork{External: map[string]string{"@external@": "80"}, Internal: map[string]string{"128": "@internal@"}},
				Volumes:     map[string]InputPackageVolume{},
				Questions: map[string]Question{
					"name":     {Query: "Who should I say hello to?"},
					"external": {Query: "What port to forward?", Type: Port},
					"internal": {Query: "What port to use internally?", Type: Port},
				},
			},
			output: Package{
				Image:       "docker.io/library/debian:latest",
				Environment: map[string]string{"HELLO": "scarlett"},
				Network:     PackageNetwork{External: PortMap{80: 80}, Internal: PortMap{128: 128}},
				Volumes:     map[string]PackageVolume{},
			},
			responses: Responses{
				"name":     "scarlett",
				"external": "80",
				"internal": "128",
			},
			err: false,
		},
		"template-errors": {
			input: InputPackage{
				Image:       "debian:latest",
				Environment: map[string]string{"HELLO": "@name@"},
				Network:     InputPackageNetwork{External: map[string]string{"@external@": "80"}, Internal: map[string]string{"128": "@internal@"}},
				Volumes:     map[string]InputPackageVolume{},
				Questions: map[string]Question{
					"name":     {Query: "Who should I say hello to?"},
					"external": {Query: "What port to forward?", Type: Port},
					"internal": {Query: "What port to use internally?", Type: Port},
				},
			},
			output: Package{},
			responses: Responses{
				"name":     "scarlett",
				"external": "-80",
				"internal": "128",
			},
			err: true,
		},
	}

	for name, data := range table {
		t.Run(name, func(t *testing.T) {
			p, err := data.input.Compile(data.responses)
			switch {
			case data.err:
				if err == nil {
					t.Fatal("error was expected but not received")
				}
			case err != nil:
				t.Fatalf("error encountered when none was expected: %v", err)
			default:
				if !reflect.DeepEqual(*p, data.output) {
					t.Fatalf("expected output was not equal to compiled output:\n  expected: %#v\n  actual:   %#v", data.output, *p)
				}
			}
		})
	}
}

func TestPackageCompileAdditional(t *testing.T) {
	t.Run("invalid response key", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{"name": {Query: "What is your name?"}},
		}
		_, err := input.Compile(Responses{"bogus": "value"})
		if err == nil {
			t.Fatal("expected error for unknown response key")
		}
	})

	t.Run("volume template substitution", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{"data": {Mountpoint: "/mnt/@path@"}},
			Questions:   map[string]Question{"path": {Query: "Mount path?"}},
		}
		p, err := input.Compile(Responses{"path": "mydata"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Volumes["data"].Mountpoint != "/mnt/mydata" {
			t.Fatalf("expected /mnt/mydata, got %s", p.Volumes["data"].Mountpoint)
		}
		if p.Image != "docker.io/library/debian:latest" {
			t.Fatalf("expected normalized image, got %s", p.Image)
		}
	})

	t.Run("port 65535 valid", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{External: map[string]string{"65535": "65535"}},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
		}
		p, err := input.Compile(Responses{})
		if err != nil {
			t.Fatalf("port 65535 should be valid: %v", err)
		}
		if p.Network.External[65535] != 65535 {
			t.Fatalf("expected port 65535 mapping, got %v", p.Network.External)
		}
	})

	t.Run("port 0 rejected", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{External: map[string]string{"0": "80"}},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
		}
		_, err := input.Compile(Responses{})
		if err == nil {
			t.Fatal("port 0 should be rejected")
		}
	})
}

func TestPackageCompileTypeValidation(t *testing.T) {
	t.Run("valid port type", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{"port": {Query: "What port?", Type: Port}},
		}
		_, err := input.Compile(Responses{"port": "8080"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("valid hostname type", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{"HOST": "@hostname@"},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{"hostname": {Query: "What hostname?", Type: Hostname}},
		}
		p, err := input.Compile(Responses{"hostname": "myhost"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Environment["HOST"] != "myhost" {
			t.Fatalf("expected HOST=myhost, got %s", p.Environment["HOST"])
		}
	})

	t.Run("valid volume type", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{"data": {Mountpoint: "/mnt/@vol@"}},
			Questions:   map[string]Question{"vol": {Query: "Volume name?", Type: Volume}},
		}
		p, err := input.Compile(Responses{"vol": "my-data"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Volumes["data"].Mountpoint != "/mnt/my-data" {
			t.Fatalf("expected /mnt/my-data, got %s", p.Volumes["data"].Mountpoint)
		}
	})

	t.Run("invalid port type", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{"port": {Query: "What port?", Type: Port}},
		}
		_, err := input.Compile(Responses{"port": "abc"})
		if err == nil {
			t.Fatal("expected error for invalid port")
		}
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("expected *ValidationError, got %T: %v", err, err)
		}
		if len(ve.Errors) != 1 || ve.Errors[0].Name != "port" {
			t.Fatalf("expected single port error, got %v", ve.Errors)
		}
	})

	t.Run("invalid hostname type", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{"hostname": {Query: "What hostname?", Type: Hostname}},
		}
		_, err := input.Compile(Responses{"hostname": "9bad"})
		if err == nil {
			t.Fatal("expected error for invalid hostname")
		}
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("expected *ValidationError, got %T: %v", err, err)
		}
		if len(ve.Errors) != 1 || ve.Errors[0].Name != "hostname" {
			t.Fatalf("expected single hostname error, got %v", ve.Errors)
		}
	})

	t.Run("valid secret type", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{"DB_PASSWORD": "@dbpass@"},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{"dbpass": {Query: "Database password?", Type: Secret}},
		}
		p, err := input.Compile(Responses{"dbpass": "s3cret"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Environment["DB_PASSWORD"] != "s3cret" {
			t.Fatalf("expected DB_PASSWORD=s3cret, got %s", p.Environment["DB_PASSWORD"])
		}
	})

	t.Run("untyped question accepts any string", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{"NAME": "@name@"},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{"name": {Query: "What is your name?"}},
		}
		p, err := input.Compile(Responses{"name": "anything at all 123!@#"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Environment["NAME"] != "anything at all 123!@#" {
			t.Fatalf("expected untyped question to accept any string, got %s", p.Environment["NAME"])
		}
	})
}

func TestPackageCompileVolumeQuota(t *testing.T) {
	t.Run("literal quota in gb", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{"data": {Mountpoint: "/data", Quota: "1gb"}},
			Questions:   map[string]Question{},
		}
		p, err := input.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Volumes["data"].Quota != 1073741824 {
			t.Fatalf("expected 1073741824, got %d", p.Volumes["data"].Quota)
		}
	})

	t.Run("literal quota in mb", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{"data": {Mountpoint: "/data", Quota: "500mb"}},
			Questions:   map[string]Question{},
		}
		p, err := input.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Volumes["data"].Quota != 524288000 {
			t.Fatalf("expected 524288000, got %d", p.Volumes["data"].Quota)
		}
	})

	t.Run("literal quota in tb", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{"data": {Mountpoint: "/data", Quota: "2tb"}},
			Questions:   map[string]Question{},
		}
		p, err := input.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Volumes["data"].Quota != 2199023255552 {
			t.Fatalf("expected 2199023255552, got %d", p.Volumes["data"].Quota)
		}
	})

	t.Run("literal quota in bytes", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{"data": {Mountpoint: "/data", Quota: "1073741824"}},
			Questions:   map[string]Question{},
		}
		p, err := input.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Volumes["data"].Quota != 1073741824 {
			t.Fatalf("expected 1073741824, got %d", p.Volumes["data"].Quota)
		}
	})

	t.Run("templated quota via bytes type", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{"data": {Mountpoint: "/data", Quota: "@size@"}},
			Questions:   map[string]Question{"size": {Query: "How much storage?", Type: Bytes}},
		}
		p, err := input.Compile(Responses{"size": "2gb"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Bytes type normalizes "2gb" to "2147483648" during template substitution,
		// then Compile parses the decimal string.
		if p.Volumes["data"].Quota != 2147483648 {
			t.Fatalf("expected 2147483648, got %d", p.Volumes["data"].Quota)
		}
	})

	t.Run("no quota is zero", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{"data": {Mountpoint: "/data"}},
			Questions:   map[string]Question{},
		}
		p, err := input.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Volumes["data"].Quota != 0 {
			t.Fatalf("expected 0, got %d", p.Volumes["data"].Quota)
		}
	})

	t.Run("invalid quota rejected", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{"data": {Mountpoint: "/data", Quota: "notasize"}},
			Questions:   map[string]Question{},
		}
		_, err := input.Compile(Responses{})
		if err == nil {
			t.Fatal("expected error for invalid quota")
		}
	})
}

func TestPackageCompileUnansweredQuestion(t *testing.T) {
	t.Run("missing response rejected", func(t *testing.T) {
		input := InputPackage{
			Image:       "nginx:1.0",
			Environment: map[string]string{"HOST": "@hostname@"},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions: map[string]Question{
				"hostname": {Query: "What hostname?", Type: Hostname},
			},
		}
		_, err := input.Compile(Responses{})
		if err == nil {
			t.Fatal("expected error for unanswered question")
		}
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("expected *ValidationError, got %T: %v", err, err)
		}
		if len(ve.Errors) != 1 || ve.Errors[0].Error != ErrMissingResponse.Error() {
			t.Fatalf("expected missing response error, got %v", ve.Errors)
		}
	})

	t.Run("partial responses rejected", func(t *testing.T) {
		input := InputPackage{
			Image:       "nginx:1.0",
			Environment: map[string]string{"HOST": "@hostname@"},
			Network:     InputPackageNetwork{External: map[string]string{"@port@": "80"}, Internal: map[string]string{}},
			Volumes:     map[string]InputPackageVolume{},
			Questions: map[string]Question{
				"hostname": {Query: "What hostname?", Type: Hostname},
				"port":     {Query: "What port?", Type: Port},
			},
		}
		_, err := input.Compile(Responses{"hostname": "example"})
		if err == nil {
			t.Fatal("expected error for partial responses")
		}
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("expected *ValidationError, got %T: %v", err, err)
		}
		if len(ve.Errors) != 1 || ve.Errors[0].Name != "port" {
			t.Fatalf("expected missing port error, got %v", ve.Errors)
		}
	})

	t.Run("all responses provided succeeds", func(t *testing.T) {
		input := InputPackage{
			Image:       "nginx:1.0",
			Environment: map[string]string{"HOST": "@hostname@"},
			Network:     InputPackageNetwork{External: map[string]string{"@port@": "80"}, Internal: map[string]string{}},
			Volumes:     map[string]InputPackageVolume{},
			Questions: map[string]Question{
				"hostname": {Query: "What hostname?", Type: Hostname},
				"port":     {Query: "What port?", Type: Port},
			},
		}
		_, err := input.Compile(Responses{"hostname": "example", "port": "8080"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("no questions no responses succeeds", func(t *testing.T) {
		input := InputPackage{
			Image:       "nginx:1.0",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
		}
		_, err := input.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("collects all validation errors", func(t *testing.T) {
		input := InputPackage{
			Image:       "nginx:1.0",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions: map[string]Question{
				"hostname": {Query: "What hostname?", Type: Hostname},
				"port":     {Query: "What port?", Type: Port},
			},
		}
		// "bogus" is unknown, "hostname" is missing, "port" is empty
		_, err := input.Compile(Responses{"bogus": "value", "port": ""})
		if err == nil {
			t.Fatal("expected error")
		}
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("expected *ValidationError, got %T: %v", err, err)
		}
		// Should have 3 errors: bogus (question does not exist), hostname (missing), port (empty)
		if len(ve.Errors) != 3 {
			t.Fatalf("expected 3 errors, got %d: %v", len(ve.Errors), ve.Errors)
		}

		errMap := map[string]string{}
		for _, e := range ve.Errors {
			errMap[e.Name] = e.Error
		}
		if errMap["bogus"] != ErrInvalidResponse.Error() {
			t.Fatalf("expected bogus error %q, got %q", ErrInvalidResponse.Error(), errMap["bogus"])
		}
		if errMap["hostname"] != ErrMissingResponse.Error() {
			t.Fatalf("expected hostname error %q, got %q", ErrMissingResponse.Error(), errMap["hostname"])
		}
		if errMap["port"] != ErrEmptyResponse.Error() {
			t.Fatalf("expected port error %q, got %q", ErrEmptyResponse.Error(), errMap["port"])
		}
	})

	t.Run("empty response rejected", func(t *testing.T) {
		input := InputPackage{
			Image:       "nginx:1.0",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{"name": {Query: "Name?"}},
		}
		_, err := input.Compile(Responses{"name": ""})
		if err == nil {
			t.Fatal("expected error for empty response")
		}
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("expected *ValidationError, got %T: %v", err, err)
		}
		if len(ve.Errors) != 1 || ve.Errors[0].Error != ErrEmptyResponse.Error() {
			t.Fatalf("expected empty response error, got %v", ve.Errors)
		}
	})
}

func TestPackageCompileCommand(t *testing.T) {
	input := InputPackage{
		Image:       "redis:7.0-alpine",
		Command:     []string{"redis-server", "--bind", "0.0.0.0"},
		Environment: map[string]string{},
		Network:     InputPackageNetwork{},
		Volumes:     map[string]InputPackageVolume{},
		Questions:   map[string]Question{},
	}
	p, err := input.Compile(Responses{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.Command) != 3 {
		t.Fatalf("expected 3 command args, got %d", len(p.Command))
	}
	if p.Command[0] != "redis-server" || p.Command[1] != "--bind" || p.Command[2] != "0.0.0.0" {
		t.Fatalf("expected [redis-server --bind 0.0.0.0], got %v", p.Command)
	}
}

func TestCompileArchiveFieldPropagation(t *testing.T) {
	t.Run("archive field propagated through compile", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{"data": {Mountpoint: "/data", Archive: "myarchive.tar.gz"}},
			Questions:   map[string]Question{},
		}
		p, err := input.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Volumes["data"].Archive != "myarchive.tar.gz" {
			t.Fatalf("expected archive myarchive.tar.gz, got %s", p.Volumes["data"].Archive)
		}
	})

	t.Run("archive field template substitution", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{"data": {Mountpoint: "/data", Archive: "@archivename@"}},
			Questions:   map[string]Question{"archivename": {Query: "Archive file?"}},
		}
		p, err := input.Compile(Responses{"archivename": "custom.tar.gz"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Volumes["data"].Archive != "custom.tar.gz" {
			t.Fatalf("expected archive custom.tar.gz, got %s", p.Volumes["data"].Archive)
		}
	})

	t.Run("no archive field is empty", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{"data": {Mountpoint: "/data"}},
			Questions:   map[string]Question{},
		}
		p, err := input.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Volumes["data"].Archive != "" {
			t.Fatalf("expected empty archive, got %s", p.Volumes["data"].Archive)
		}
	})
}

func TestCompileGitFieldPropagation(t *testing.T) {
	t.Run("git field propagated through compile", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{"config": {Mountpoint: "/config", Git: "https://github.com/example/repo.git"}},
			Questions:   map[string]Question{},
		}
		p, err := input.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Volumes["config"].Git != "https://github.com/example/repo.git" {
			t.Fatalf("expected git URL, got %s", p.Volumes["config"].Git)
		}
	})

	t.Run("git field template substitution", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{"config": {Mountpoint: "/config", Git: "@giturl@"}},
			Questions:   map[string]Question{"giturl": {Query: "Git repo URL?"}},
		}
		p, err := input.Compile(Responses{"giturl": "https://github.com/example/custom.git"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Volumes["config"].Git != "https://github.com/example/custom.git" {
			t.Fatalf("expected custom git URL, got %s", p.Volumes["config"].Git)
		}
	})

	t.Run("no git field is empty", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{"data": {Mountpoint: "/data"}},
			Questions:   map[string]Question{},
		}
		p, err := input.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Volumes["data"].Git != "" {
			t.Fatalf("expected empty git, got %s", p.Volumes["data"].Git)
		}
	})

	t.Run("invalid git URL rejected", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{"config": {Mountpoint: "/config", Git: "not-a-url"}},
			Questions:   map[string]Question{},
		}
		_, err := input.Compile(Responses{})
		if err == nil {
			t.Fatal("expected error for invalid git URL")
		}
		if !errors.Is(err, ErrInvalidGitURL) {
			t.Fatalf("expected ErrInvalidGitURL, got %v", err)
		}
	})

	t.Run("file URL accepted", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{"config": {Mountpoint: "/config", Git: "file:///tmp/repo.git"}},
			Questions:   map[string]Question{},
		}
		p, err := input.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Volumes["config"].Git != "file:///tmp/repo.git" {
			t.Fatalf("expected file URL, got %s", p.Volumes["config"].Git)
		}
	})

	t.Run("git coexists with archive", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes: map[string]InputPackageVolume{
				"data":   {Mountpoint: "/data", Archive: "backup.tar.gz"},
				"config": {Mountpoint: "/config", Git: "https://github.com/example/config.git"},
			},
			Questions: map[string]Question{},
		}
		p, err := input.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Volumes["data"].Archive != "backup.tar.gz" {
			t.Fatalf("expected archive backup.tar.gz, got %s", p.Volumes["data"].Archive)
		}
		if p.Volumes["config"].Git != "https://github.com/example/config.git" {
			t.Fatalf("expected git URL, got %s", p.Volumes["config"].Git)
		}
	})
}

func TestCompileArchivesField(t *testing.T) {
	t.Run("archives parsed and validated", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{"data": {Mountpoint: "/data"}},
			Questions:   map[string]Question{},
			Archives: []InputPackageArchive{
				{Image: "nginx:latest", Directory: "/usr/share/nginx/html", Volume: "data"},
			},
		}
		_, err := input.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("archives with invalid volume rejected", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{"data": {Mountpoint: "/data"}},
			Questions:   map[string]Question{},
			Archives: []InputPackageArchive{
				{Image: "nginx:latest", Directory: "/data", Volume: "nonexistent"},
			},
		}
		_, err := input.Compile(Responses{})
		if err == nil {
			t.Fatal("expected error for invalid archive volume reference")
		}
	})
}

func TestCompileNotes(t *testing.T) {
	t.Run("templates notes with responses", func(t *testing.T) {
		input := InputPackage{
			Image:       "nginx:1.0",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
			Notes:       map[string]Note{"URL": {Value: "http://@hostname@:@port@", Type: NoteURL}},
		}
		notes, err := input.CompileNotes(Responses{"hostname": "example.com", "port": "8080"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if notes["URL"] != "http://example.com:8080" {
			t.Fatalf("expected http://example.com:8080, got %s", notes["URL"])
		}
	})

	t.Run("nil notes returns nil", func(t *testing.T) {
		input := InputPackage{
			Image:       "nginx:1.0",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
		}
		notes, err := input.CompileNotes(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(notes) != 0 {
			t.Fatalf("expected empty, got %v", notes)
		}
	})

	t.Run("empty notes returns nil", func(t *testing.T) {
		input := InputPackage{
			Image:       "nginx:1.0",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
			Notes:       map[string]Note{},
		}
		notes, err := input.CompileNotes(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(notes) != 0 {
			t.Fatalf("expected empty, got %v", notes)
		}
	})

	t.Run("notes with no templates pass through", func(t *testing.T) {
		input := InputPackage{
			Image:       "nginx:1.0",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
			Notes:       map[string]Note{"Info": {Value: "static text"}},
		}
		notes, err := input.CompileNotes(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if notes["Info"] != "static text" {
			t.Fatalf("expected 'static text', got %s", notes["Info"])
		}
	})

	t.Run("valid URL note with template", func(t *testing.T) {
		input := InputPackage{
			Image:       "nginx:1.0",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
			Notes:       map[string]Note{"URL": {Value: "http://@host@:@port@", Type: NoteURL}},
		}
		notes, err := input.CompileNotes(Responses{"host": "myhost", "port": "9090"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if notes["URL"] != "http://myhost:9090" {
			t.Fatalf("expected http://myhost:9090, got %s", notes["URL"])
		}
	})

	t.Run("valid phone note", func(t *testing.T) {
		input := InputPackage{
			Image:       "nginx:1.0",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
			Notes:       map[string]Note{"Phone": {Value: "+1 (555) 123-4567", Type: NotePhone}},
		}
		notes, err := input.CompileNotes(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if notes["Phone"] != "+1 (555) 123-4567" {
			t.Fatalf("expected +1 (555) 123-4567, got %s", notes["Phone"])
		}
	})

	t.Run("valid email note", func(t *testing.T) {
		input := InputPackage{
			Image:       "nginx:1.0",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
			Notes:       map[string]Note{"Email": {Value: "admin@example.com", Type: NoteEmail}},
		}
		notes, err := input.CompileNotes(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if notes["Email"] != "admin@example.com" {
			t.Fatalf("expected admin@example.com, got %s", notes["Email"])
		}
	})

	t.Run("email note via template", func(t *testing.T) {
		input := InputPackage{
			Image:       "nginx:1.0",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
			Notes:       map[string]Note{"Email": {Value: "@email@", Type: NoteEmail}},
		}
		notes, err := input.CompileNotes(Responses{"email": "admin@example.com"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if notes["Email"] != "admin@example.com" {
			t.Fatalf("expected admin@example.com, got %s", notes["Email"])
		}
	})

	t.Run("invalid URL note", func(t *testing.T) {
		input := InputPackage{
			Image:       "nginx:1.0",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
			Notes:       map[string]Note{"URL": {Value: "://missing-scheme", Type: NoteURL}},
		}
		_, err := input.CompileNotes(Responses{})
		if err == nil {
			t.Fatal("expected error for invalid URL note")
		}
		if !errors.Is(err, ErrInvalidNoteURL) {
			t.Fatalf("expected ErrInvalidNoteURL, got %v", err)
		}
	})

	t.Run("invalid phone note", func(t *testing.T) {
		input := InputPackage{
			Image:       "nginx:1.0",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
			Notes:       map[string]Note{"Phone": {Value: "abc", Type: NotePhone}},
		}
		_, err := input.CompileNotes(Responses{})
		if err == nil {
			t.Fatal("expected error for invalid phone note")
		}
		if !errors.Is(err, ErrInvalidNotePhone) {
			t.Fatalf("expected ErrInvalidNotePhone, got %v", err)
		}
	})

	t.Run("invalid email note", func(t *testing.T) {
		input := InputPackage{
			Image:       "nginx:1.0",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
			Notes:       map[string]Note{"Email": {Value: "nope", Type: NoteEmail}},
		}
		_, err := input.CompileNotes(Responses{})
		if err == nil {
			t.Fatal("expected error for invalid email note")
		}
		if !errors.Is(err, ErrInvalidNoteEmail) {
			t.Fatalf("expected ErrInvalidNoteEmail, got %v", err)
		}
	})

	t.Run("untyped note passthrough", func(t *testing.T) {
		input := InputPackage{
			Image:       "nginx:1.0",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
			Notes:       map[string]Note{"Info": {Value: "anything"}},
		}
		notes, err := input.CompileNotes(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if notes["Info"] != "anything" {
			t.Fatalf("expected 'anything', got %s", notes["Info"])
		}
	})
}
