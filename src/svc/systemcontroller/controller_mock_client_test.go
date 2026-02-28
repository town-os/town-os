package systemcontroller

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
)

func TestMockClientCreateAndList(t *testing.T) {
	m := InitMockClient()

	if err := m.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "test"}); err != nil {
		t.Fatalf("MockClient.CreateFilesystem %q: %v", "test", err)
	}

	fsResult, err := m.ListFilesystems(context.TODO(), "", "", ListParams{})
	if err != nil {
		t.Fatalf("MockClient.ListFilesystems: %v", err)
	}

	if len(fsResult.Entries) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(fsResult.Entries))
	}

	if fsResult.Entries[0].Name != "test" {
		t.Fatalf("expected name %q, got %q", "test", fsResult.Entries[0].Name)
	}
}

func TestMockClientRemove(t *testing.T) {
	m := InitMockClient()

	if err := m.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "test"}); err != nil {
		t.Fatalf("MockClient.CreateFilesystem %q: %v", "test", err)
	}
	if err := m.RemoveFilesystem(context.TODO(), "test"); err != nil {
		t.Fatalf("MockClient.RemoveFilesystem %q: %v", "test", err)
	}

	fsResult, err := m.ListFilesystems(context.TODO(), "", "", ListParams{})
	if err != nil {
		t.Fatalf("MockClient.ListFilesystems: %v", err)
	}

	if len(fsResult.Entries) != 0 {
		t.Fatalf("expected 0 filesystems, got %d", len(fsResult.Entries))
	}
}

func TestMockClientModify(t *testing.T) {
	m := InitMockClient()

	if err := m.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "test"}); err != nil {
		t.Fatalf("MockClient.CreateFilesystem %q: %v", "test", err)
	}

	if err := m.ModifyFilesystem(context.TODO(), "test", storage.Filesystem{Name: "test", Quota: 2048}); err != nil {
		t.Fatalf("MockClient.ModifyFilesystem %q: %v", "test", err)
	}

	if m.Filesystems["test"].Quota != 2048 {
		t.Fatalf("expected quota 2048, got %d", m.Filesystems["test"].Quota)
	}
}

func TestMockClientModifyNotFound(t *testing.T) {
	m := InitMockClient()

	err := m.ModifyFilesystem(context.TODO(), "nope", storage.Filesystem{Name: "nope"})
	if err == nil {
		t.Fatal("expected error modifying nonexistent filesystem")
	}
}

func TestMockClientListWithPrefix(t *testing.T) {
	m := InitMockClient()

	for _, name := range []string{"app-web", "app-db", "data-cache"} {
		if err := m.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("MockClient.CreateFilesystem %q: %v", name, err)
		}
	}

	fsResult, err := m.ListFilesystems(context.TODO(), "app-", "", ListParams{})
	if err != nil {
		t.Fatalf("MockClient.ListFilesystems %q: %v", "app-", err)
	}

	if len(fsResult.Entries) != 2 {
		t.Fatalf("expected 2 filesystems with prefix, got %d", len(fsResult.Entries))
	}

	names := map[string]bool{}
	for _, f := range fsResult.Entries {
		names[f.Name] = true
	}
	for _, want := range []string{"app-web", "app-db"} {
		if !names[want] {
			t.Fatalf("expected filesystem %q in results", want)
		}
	}
}

func TestMockClientErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := errors.New("injected error")

	m.CreateErr = injected
	if err := m.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "test"}); !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}

	m.CreateErr = nil
	m.ListErr = injected
	if _, err := m.ListFilesystems(context.TODO(), "", "", ListParams{}); !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}

	m.ListErr = nil
	m.RemoveErr = injected
	if err := m.RemoveFilesystem(context.TODO(), "test"); !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}

	m.RemoveErr = nil
	m.ModifyErr = injected
	if err := m.ModifyFilesystem(context.TODO(), "test", storage.Filesystem{}); !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientCallLog(t *testing.T) {
	m := InitMockClient()

	if err := m.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "a"}); err != nil {
		t.Fatalf("MockClient.CreateFilesystem %q: %v", "a", err)
	}
	if err := m.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "b"}); err != nil {
		t.Fatalf("MockClient.CreateFilesystem %q: %v", "b", err)
	}
	if _, err := m.ListFilesystems(context.TODO(), "", "", ListParams{}); err != nil {
		t.Fatalf("MockClient.ListFilesystems: %v", err)
	}
	if err := m.RemoveFilesystem(context.TODO(), "a"); err != nil {
		t.Fatalf("MockClient.RemoveFilesystem %q: %v", "a", err)
	}

	calls := m.GetCalls()
	if len(calls) != 4 {
		t.Fatalf("expected 4 calls, got %d", len(calls))
	}

	expected := []string{"CreateFilesystem", "CreateFilesystem", "ListFilesystems", "RemoveFilesystem"}
	for i, want := range expected {
		if calls[i].Method != want {
			t.Fatalf("call %d: expected method %q, got %q", i, want, calls[i].Method)
		}
	}
}

func TestMockClientAddAndListRepositories(t *testing.T) {
	m := InitMockClient()

	if err := m.AddRepository(context.TODO(), "", "https://example.com/repo.git", "", ""); err != nil {
		t.Fatalf("MockClient.AddRepository: %v", err)
	}

	repos, err := m.ListRepositories(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("MockClient.ListRepositories: %v", err)
	}

	if len(repos.Entries) != 1 {
		t.Fatalf("expected 1 repository, got %d", len(repos.Entries))
	}

	if repos.Entries[0].URL != "https://example.com/repo.git" {
		t.Fatalf("expected URL %q, got %q", "https://example.com/repo.git", repos.Entries[0].URL)
	}
}

func TestMockClientAddDuplicateRepository(t *testing.T) {
	m := InitMockClient()

	if err := m.AddRepository(context.TODO(), "", "https://example.com/repo.git", "", ""); err != nil {
		t.Fatalf("MockClient.AddRepository: %v", err)
	}

	err := m.AddRepository(context.TODO(), "", "https://example.com/repo.git", "", "")
	if err == nil {
		t.Fatal("expected error adding duplicate repository")
	}
}

func TestMockClientRemoveRepository(t *testing.T) {
	m := InitMockClient()

	if err := m.AddRepository(context.TODO(), "", "https://example.com/repo.git", "", ""); err != nil {
		t.Fatalf("MockClient.AddRepository: %v", err)
	}

	if err := m.RemoveRepository(context.TODO(), "https://example.com/repo.git"); err != nil {
		t.Fatalf("MockClient.RemoveRepository: %v", err)
	}

	repos, err := m.ListRepositories(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("MockClient.ListRepositories: %v", err)
	}

	if len(repos.Entries) != 0 {
		t.Fatalf("expected 0 repositories, got %d", len(repos.Entries))
	}
}

func TestMockClientRemoveRepositoryNotFound(t *testing.T) {
	m := InitMockClient()

	err := m.RemoveRepository(context.TODO(), "nonexistent")
	if err == nil {
		t.Fatal("expected error removing nonexistent repository")
	}
}

func TestMockClientRepositoryErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := errors.New("injected error")

	m.AddRepoErr = injected
	if err := m.AddRepository(context.TODO(), "", "https://example.com/repo.git", "", ""); !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}

	m.AddRepoErr = nil
	m.RemRepoErr = injected
	if err := m.RemoveRepository(context.TODO(), "test"); !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}

	m.RemRepoErr = nil
	m.ListRepoErr = injected
	if _, err := m.ListRepositories(context.TODO(), ListParams{}); !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientRepositoryCallLog(t *testing.T) {
	m := InitMockClient()

	if err := m.AddRepository(context.TODO(), "", "https://example.com/a.git", "", ""); err != nil {
		t.Fatalf("MockClient.AddRepository %q: %v", "https://example.com/a.git", err)
	}
	if err := m.AddRepository(context.TODO(), "", "https://example.com/b.git", "", ""); err != nil {
		t.Fatalf("MockClient.AddRepository %q: %v", "https://example.com/b.git", err)
	}
	if _, err := m.ListRepositories(context.TODO(), ListParams{}); err != nil {
		t.Fatalf("MockClient.ListRepositories: %v", err)
	}
	if err := m.RemoveRepository(context.TODO(), "https://example.com/a.git"); err != nil {
		t.Fatalf("MockClient.RemoveRepository %q: %v", "https://example.com/a.git", err)
	}

	calls := m.GetCalls()
	if len(calls) != 4 {
		t.Fatalf("expected 4 calls, got %d", len(calls))
	}

	expected := []string{"AddRepository", "AddRepository", "ListRepositories", "RemoveRepository"}
	for i, want := range expected {
		if calls[i].Method != want {
			t.Fatalf("call %d: expected method %q, got %q", i, want, calls[i].Method)
		}
	}
}

func TestMockClientListPackages(t *testing.T) {
	m := InitMockClient()
	m.Packages = []string{"mock-repo/nginx@2.0", "mock-repo/redis@7.0"}

	pkgs, err := m.ListPackages(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("MockClient.ListPackages: %v", err)
	}

	if len(pkgs.Entries) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(pkgs.Entries))
	}

	if pkgs.Entries[0].Repo != "mock-repo" || pkgs.Entries[0].Name != "nginx" || pkgs.Entries[0].Version != "2.0" {
		t.Fatalf("expected mock-repo/nginx@2.0, got %s/%s@%s", pkgs.Entries[0].Repo, pkgs.Entries[0].Name, pkgs.Entries[0].Version)
	}
}

func TestMockClientListPackagesEmpty(t *testing.T) {
	m := InitMockClient()

	pkgs, err := m.ListPackages(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("MockClient.ListPackages: %v", err)
	}

	if len(pkgs.Entries) != 0 {
		t.Fatalf("expected 0 packages, got %d", len(pkgs.Entries))
	}
}

func TestMockClientListPackagesErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := errors.New("injected error")

	m.ListPkgErr = injected
	if _, err := m.ListPackages(context.TODO(), ListParams{}); !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientListPackagesCallLog(t *testing.T) {
	m := InitMockClient()
	m.Packages = []string{"mock-repo/nginx@1.0"}

	if _, err := m.ListPackages(context.TODO(), ListParams{}); err != nil {
		t.Fatalf("MockClient.ListPackages (first call): %v", err)
	}
	if _, err := m.ListPackages(context.TODO(), ListParams{}); err != nil {
		t.Fatalf("MockClient.ListPackages (second call): %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}

	for _, c := range calls {
		if c.Method != "ListPackages" {
			t.Fatalf("expected method ListPackages, got %q", c.Method)
		}
	}
}

func TestMockClientGetPackageQuestionsWithSecret(t *testing.T) {
	m := InitMockClient()
	m.Questions = map[string]map[string]packages.Question{
		"myapp": {
			"dbpass": {Query: "Database password?", Type: packages.Secret},
		},
	}

	questions, err := m.GetPackageQuestions(context.TODO(), "myapp")
	if err != nil {
		t.Fatalf("MockClient.GetPackageQuestions: %v", err)
	}

	if len(questions) != 1 {
		t.Fatalf("expected 1 question, got %d", len(questions))
	}

	if questions["dbpass"].Type != packages.Secret {
		t.Fatalf("expected secret type, got %q", questions["dbpass"].Type)
	}
	if questions["dbpass"].Query != "Database password?" {
		t.Fatalf("expected %q, got %q", "Database password?", questions["dbpass"].Query)
	}
}

func TestMockClientGetPackageQuestions(t *testing.T) {
	m := InitMockClient()
	m.Questions = map[string]map[string]packages.Question{
		"nginx": {
			"hostname": {Query: "What hostname?", Type: packages.Hostname},
			"port":     {Query: "What port?", Type: packages.Port},
		},
	}

	questions, err := m.GetPackageQuestions(context.TODO(), "nginx")
	if err != nil {
		t.Fatalf("MockClient.GetPackageQuestions: %v", err)
	}

	if len(questions) != 2 {
		t.Fatalf("expected 2 questions, got %d", len(questions))
	}

	if questions["hostname"].Query != "What hostname?" {
		t.Fatalf("expected %q, got %q", "What hostname?", questions["hostname"].Query)
	}
	if questions["port"].Type != packages.Port {
		t.Fatalf("expected port type, got %q", questions["port"].Type)
	}
}

func TestMockClientGetPackageQuestionsNotFound(t *testing.T) {
	m := InitMockClient()

	_, err := m.GetPackageQuestions(context.TODO(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent package")
	}
}

func TestMockClientGetPackageQuestionsErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := errors.New("injected error")

	m.Questions = map[string]map[string]packages.Question{
		"nginx": {
			"hostname": {Query: "What hostname?"},
		},
	}

	m.QuestionsErr = injected
	if _, err := m.GetPackageQuestions(context.TODO(), "nginx"); !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientGetPackageQuestionsCallLog(t *testing.T) {
	m := InitMockClient()
	m.Questions = map[string]map[string]packages.Question{
		"nginx": {
			"hostname": {Query: "What hostname?"},
		},
	}

	if _, err := m.GetPackageQuestions(context.TODO(), "nginx"); err != nil {
		t.Fatalf("MockClient.GetPackageQuestions: %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}

	if calls[0].Method != "GetPackageQuestions" {
		t.Fatalf("expected method GetPackageQuestions, got %q", calls[0].Method)
	}

	args := calls[0].Args
	if len(args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(args))
	}
	argStr, ok := args[0].(string)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if argStr != "nginx" {
		t.Fatalf("expected arg %q, got %v", "nginx", args[0])
	}
}

func TestMockClientGetPackageQuestionsReturnsCopy(t *testing.T) {
	m := InitMockClient()
	m.Questions = map[string]map[string]packages.Question{
		"nginx": {
			"hostname": {Query: "What hostname?"},
		},
	}

	questions, err := m.GetPackageQuestions(context.TODO(), "nginx")
	if err != nil {
		t.Fatalf("MockClient.GetPackageQuestions: %v", err)
	}

	questions["hostname"] = packages.Question{Query: "mutated"}

	if m.Questions["nginx"]["hostname"].Query != "What hostname?" {
		t.Fatal("GetPackageQuestions should return a copy, not a reference")
	}
}

func TestMockClientGetPackageQuestionsByIdentity(t *testing.T) {
	m := InitMockClient()
	m.Questions = map[string]map[string]packages.Question{
		"nginx@1.0": {
			"hostname": {Query: "What hostname?", Type: packages.Hostname},
		},
	}

	questions, err := m.GetPackageQuestionsByIdentity(context.TODO(), "mock-repo", "nginx", "1.0")
	if err != nil {
		t.Fatalf("MockClient.GetPackageQuestionsByIdentity: %v", err)
	}

	if len(questions) != 1 {
		t.Fatalf("expected 1 question, got %d", len(questions))
	}
	if questions["hostname"].Query != "What hostname?" {
		t.Fatalf("expected %q, got %q", "What hostname?", questions["hostname"].Query)
	}
}

func TestMockClientGetPackageQuestionsByIdentityNotFound(t *testing.T) {
	m := InitMockClient()

	_, err := m.GetPackageQuestionsByIdentity(context.TODO(), "mock-repo", "nginx", "1.0")
	if err == nil {
		t.Fatal("expected error for nonexistent package")
	}
}

func TestMockClientGetPackageQuestionsByIdentityErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := errors.New("injected error")

	m.Questions = map[string]map[string]packages.Question{
		"nginx@1.0": {
			"hostname": {Query: "What hostname?"},
		},
	}

	m.QuestionsIdentityErr = injected
	if _, err := m.GetPackageQuestionsByIdentity(context.TODO(), "mock-repo", "nginx", "1.0"); !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientInstallPackage(t *testing.T) {
	m := InitMockClient()

	responses := packages.Responses{"hostname": "example"}
	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", responses, false, "", false); err != nil {
		t.Fatalf("MockClient.InstallPackage: %v", err)
	}

	if len(m.Installed) != 1 {
		t.Fatalf("expected 1 installed, got %d", len(m.Installed))
	}
	if m.Installed[0] != "nginx@1.0" {
		t.Fatalf("expected nginx@1.0, got %s", m.Installed[0])
	}
	if m.StoredResponses["nginx@1.0"]["hostname"] != "example" {
		t.Fatalf("expected stored response hostname=example.com, got %v", m.StoredResponses["nginx@1.0"])
	}
}

func TestMockClientInstallPackageErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := errors.New("injected error")

	m.InstallPkgErr = injected
	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, "", false); !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientInstallPackageCallLog(t *testing.T) {
	m := InitMockClient()

	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"k": "v"}, false, "", false); err != nil {
		t.Fatalf("MockClient.InstallPackage: %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Method != "InstallPackage" {
		t.Fatalf("expected method InstallPackage, got %q", calls[0].Method)
	}
	if len(calls[0].Args) != 6 {
		t.Fatalf("expected 6 args, got %d", len(calls[0].Args))
	}
	arg0, ok := calls[0].Args[0].(string)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if arg0 != "nginx" {
		t.Fatalf("expected arg 0 %q, got %v", "nginx", calls[0].Args[0])
	}
	arg1, ok := calls[0].Args[1].(string)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if arg1 != "1.0" {
		t.Fatalf("expected arg 1 %q, got %v", "1.0", calls[0].Args[1])
	}
}

func TestMockClientUninstallPackage(t *testing.T) {
	m := InitMockClient()

	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	if err := m.UninstallPackage(context.TODO(), "mock-repo", "nginx", "1.0", false); err != nil {
		t.Fatalf("MockClient.UninstallPackage: %v", err)
	}

	if len(m.Installed) != 0 {
		t.Fatalf("expected 0 installed after uninstall, got %d", len(m.Installed))
	}
}

func TestMockClientUninstallPackageNotInstalled(t *testing.T) {
	m := InitMockClient()

	err := m.UninstallPackage(context.TODO(), "mock-repo", "nginx", "1.0", false)
	if err == nil {
		t.Fatal("expected error uninstalling non-installed package")
	}
}

func TestMockClientUninstallPackageErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := errors.New("injected error")

	m.UninstallPkgErr = injected
	if err := m.UninstallPackage(context.TODO(), "mock-repo", "nginx", "1.0", false); !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientUninstallPackageCallLog(t *testing.T) {
	m := InitMockClient()

	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	if err := m.UninstallPackage(context.TODO(), "mock-repo", "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[1].Method != "UninstallPackage" {
		t.Fatalf("expected method UninstallPackage, got %q", calls[1].Method)
	}
	if len(calls[1].Args) != 4 {
		t.Fatalf("expected 4 args, got %d", len(calls[1].Args))
	}
}

func TestMockClientUninstallPackagePurgesVolumes(t *testing.T) {
	m := InitMockClient()

	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	m.Filesystems["installed/mock-repo/nginx"] = storage.Filesystem{Name: "installed/mock-repo/nginx"}
	m.Filesystems["installed/mock-repo/nginx/1.0/html"] = storage.Filesystem{Name: "installed/mock-repo/nginx/1.0/html", Quota: 1024}
	m.Filesystems["installed/mock-repo/nginx/1.0/logs"] = storage.Filesystem{Name: "installed/mock-repo/nginx/1.0/logs", Quota: 2048}
	m.Filesystems["installed/other/1.0/data"] = storage.Filesystem{Name: "installed/other/1.0/data"}

	if err := m.UninstallPackage(context.TODO(), "mock-repo", "nginx", "1.0", true); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}

	if len(m.Installed) != 0 {
		t.Fatalf("expected 0 installed after uninstall, got %d", len(m.Installed))
	}

	if _, ok := m.Filesystems["installed/mock-repo/nginx"]; ok {
		t.Fatal("expected installed/mock-repo/nginx parent volume to be purged")
	}
	if _, ok := m.Filesystems["installed/mock-repo/nginx/1.0/html"]; ok {
		t.Fatal("expected installed/mock-repo/nginx/1.0/html volume to be purged")
	}
	if _, ok := m.Filesystems["installed/mock-repo/nginx/1.0/logs"]; ok {
		t.Fatal("expected installed/mock-repo/nginx/1.0/logs volume to be purged")
	}
	if _, ok := m.Filesystems["installed/other/1.0/data"]; !ok {
		t.Fatal("expected installed/other/1.0/data volume to be preserved")
	}
}

func TestMockClientUninstallPackageNoPurge(t *testing.T) {
	m := InitMockClient()

	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	m.Filesystems["installed/mock-repo/nginx/1.0/html"] = storage.Filesystem{Name: "installed/mock-repo/nginx/1.0/html", Quota: 1024}

	if err := m.UninstallPackage(context.TODO(), "mock-repo", "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}

	if _, ok := m.Filesystems["installed/mock-repo/nginx/1.0/html"]; !ok {
		t.Fatal("expected installed/mock-repo/nginx/1.0/html volume to be preserved when purge is false")
	}
}

func TestMockClientPurgeVolumes(t *testing.T) {
	m := InitMockClient()

	m.Filesystems["installed/mock-repo/nginx"] = storage.Filesystem{Name: "installed/mock-repo/nginx"}
	m.Filesystems["installed/mock-repo/nginx/1.0/html"] = storage.Filesystem{Name: "installed/mock-repo/nginx/1.0/html"}
	m.Filesystems["installed/mock-repo/nginx/1.0/logs"] = storage.Filesystem{Name: "installed/mock-repo/nginx/1.0/logs"}
	m.Filesystems["installed/other/1.0/data"] = storage.Filesystem{Name: "installed/other/1.0/data"}

	if err := m.PurgeVolumes(context.TODO(), "mock-repo", "nginx"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	if _, ok := m.Filesystems["installed/mock-repo/nginx"]; ok {
		t.Fatal("expected installed/mock-repo/nginx parent volume to be purged")
	}
	if _, ok := m.Filesystems["installed/mock-repo/nginx/1.0/html"]; ok {
		t.Fatal("expected installed/mock-repo/nginx/1.0/html volume to be purged")
	}
	if _, ok := m.Filesystems["installed/mock-repo/nginx/1.0/logs"]; ok {
		t.Fatal("expected installed/mock-repo/nginx/1.0/logs volume to be purged")
	}
	if _, ok := m.Filesystems["installed/other/1.0/data"]; !ok {
		t.Fatal("expected installed/other/1.0/data volume to be preserved")
	}

	calls := m.GetCalls()
	if len(calls) != 1 || calls[0].Method != "PurgeVolumes" {
		t.Fatalf("expected 1 PurgeVolumes call, got %v", calls)
	}
}

func TestMockClientListInstalled(t *testing.T) {
	m := InitMockClient()

	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}
	if err := m.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	pkgs, err := m.ListInstalled(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("MockClient.ListInstalled: %v", err)
	}

	if len(pkgs.Entries) != 2 {
		t.Fatalf("expected 2 installed, got %d", len(pkgs.Entries))
	}
}

func TestMockClientListInstalledEmpty(t *testing.T) {
	m := InitMockClient()

	pkgs, err := m.ListInstalled(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("MockClient.ListInstalled: %v", err)
	}

	if len(pkgs.Entries) != 0 {
		t.Fatalf("expected 0 installed, got %d", len(pkgs.Entries))
	}
}

func TestMockClientListInstalledErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := errors.New("injected error")

	m.ListInstalledErr = injected
	if _, err := m.ListInstalled(context.TODO(), ListParams{}); !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientGetResponses(t *testing.T) {
	m := InitMockClient()

	responses := packages.Responses{"hostname": "example", "port": "8080"}
	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", responses, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	got, err := m.GetResponses(context.TODO(), "mock-repo", "nginx", "1.0")
	if err != nil {
		t.Fatalf("MockClient.GetResponses: %v", err)
	}

	if got["hostname"] != "example" {
		t.Fatalf("expected hostname %q, got %q", "example", got["hostname"])
	}
	if got["port"] != "8080" {
		t.Fatalf("expected port %q, got %q", "8080", got["port"])
	}
}

func TestMockClientGetResponsesNotInstalled(t *testing.T) {
	m := InitMockClient()

	_, err := m.GetResponses(context.TODO(), "mock-repo", "nginx", "1.0")
	if err == nil {
		t.Fatal("expected error for non-installed package")
	}
}

func TestMockClientGetResponsesErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := errors.New("injected error")

	m.GetResponsesErr = injected
	if _, err := m.GetResponses(context.TODO(), "mock-repo", "nginx", "1.0"); !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientGetResponsesReturnsCopy(t *testing.T) {
	m := InitMockClient()

	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	got, err := m.GetResponses(context.TODO(), "mock-repo", "nginx", "1.0")
	if err != nil {
		t.Fatalf("GetResponses: %v", err)
	}

	got["hostname"] = "mutated"

	if m.StoredResponses["nginx@1.0"]["hostname"] != "example" {
		t.Fatal("GetResponses should return a copy, not a reference")
	}
}

func TestMockClientGetLastResponses(t *testing.T) {
	m := InitMockClient()

	resp, err := m.GetLastResponses(context.TODO(), "repo", "nginx")
	if err != nil {
		t.Fatalf("GetLastResponses: %v", err)
	}
	if len(resp) != 0 {
		t.Fatalf("expected empty last responses, got %d", len(resp))
	}
}

func TestMockClientGetLastResponsesCallLog(t *testing.T) {
	m := InitMockClient()

	if _, err := m.GetLastResponses(context.TODO(), "repo", "nginx"); err != nil {
		t.Fatalf("GetLastResponses: %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Method != "GetLastResponses" {
		t.Fatalf("expected method GetLastResponses, got %q", calls[0].Method)
	}
}

func TestMockClientClearLastResponses(t *testing.T) {
	m := InitMockClient()

	if err := m.ClearLastResponses(context.TODO(), "repo", "nginx"); err != nil {
		t.Fatalf("ClearLastResponses: %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Method != "ClearLastResponses" {
		t.Fatalf("expected method ClearLastResponses, got %q", calls[0].Method)
	}
}

func TestMockClientSettingsDefaults(t *testing.T) {
	m := InitMockClient()

	// Defaults should be present immediately.
	val, err := m.GetSetting(context.TODO(), "default_quota")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if val != account.DefaultSettings["default_quota"] {
		t.Fatalf("expected %q, got %q", account.DefaultSettings["default_quota"], val)
	}

	settings, err := m.GetSettings(context.TODO())
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if len(settings) != len(account.DefaultSettings) {
		t.Fatalf("expected %d settings, got %d", len(account.DefaultSettings), len(settings))
	}
}

func TestMockClientSettingsOverride(t *testing.T) {
	m := InitMockClient()

	if err := m.SetSetting(context.TODO(), "default_quota", "50"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	val, err := m.GetSetting(context.TODO(), "default_quota")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if val != "50" {
		t.Fatalf("expected %q, got %q", "50", val)
	}
}

func TestMockClientGetSettingNotFound(t *testing.T) {
	m := InitMockClient()

	_, err := m.GetSetting(context.TODO(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent setting")
	}
}

func TestMockClientUploadArchive(t *testing.T) {
	m := InitMockClient()
	result, err := m.UploadArchive(context.TODO(), "my-vol", strings.NewReader("fake"), "test.tar.gz", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.NeedsRestart {
		t.Fatal("expected NeedsRestart to be true")
	}

	calls := m.GetCalls()
	found := false
	for _, c := range calls {
		if c.Method == "UploadArchive" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected UploadArchive call logged")
	}
}

func TestMockClientUploadArchiveError(t *testing.T) {
	m := InitMockClient()
	m.UploadArchiveErr = errors.New("upload failed")
	_, err := m.UploadArchive(context.TODO(), "my-vol", strings.NewReader("fake"), "test.tar.gz", "", "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMockClientDownloadArchive(t *testing.T) {
	m := InitMockClient()
	reader, err := m.DownloadArchive(context.TODO(), "my-vol", nil, "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { if err := reader.Close(); err != nil { t.Errorf("reader.Close: %v", err) } }()

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty data")
	}
}

func TestMockClientDownloadArchiveError(t *testing.T) {
	m := InitMockClient()
	m.DownloadArchiveErr = errors.New("download failed")
	_, err := m.DownloadArchive(context.TODO(), "my-vol", nil, "", "", "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMockClientDownloadArchiveWithFilename(t *testing.T) {
	m := InitMockClient()
	reader, err := m.DownloadArchive(context.TODO(), "my-vol", nil, "", "tar.gz", "my-backup")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { if err := reader.Close(); err != nil { t.Errorf("reader.Close: %v", err) } }()

	// Verify the filename argument was recorded.
	m.mu.Lock()
	defer m.mu.Unlock()
	found := false
	for _, c := range m.Calls {
		if c.Method == "DownloadArchive" && len(c.Args) >= 5 {
			if fn, ok := c.Args[4].(string); ok && fn == "my-backup" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("expected filename argument to be recorded in mock calls")
	}
}

func TestMockClientListFeaturedPackagesEmpty(t *testing.T) {
	m := InitMockClient()

	groups, err := m.ListFeaturedPackages(context.TODO())
	if err != nil {
		t.Fatalf("ListFeaturedPackages: %v", err)
	}

	if len(groups) != 0 {
		t.Fatalf("expected 0 groups, got %d", len(groups))
	}
}

func TestMockClientListFeaturedPackagesWithData(t *testing.T) {
	m := InitMockClient()
	m.FeaturedGroups = []FeaturedRepoGroup{
		{
			Repo: "core",
			Packages: []FeaturedPackageEntry{
				{Repo: "core", Name: "nginx", Version: "1.0", Description: "Web server"},
			},
		},
	}

	groups, err := m.ListFeaturedPackages(context.TODO())
	if err != nil {
		t.Fatalf("ListFeaturedPackages: %v", err)
	}

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Packages[0].Name != "nginx" {
		t.Fatalf("expected nginx, got %s", groups[0].Packages[0].Name)
	}
}

func TestMockClientListFeaturedPackagesErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := errors.New("injected error")
	m.ListFeaturedErr = injected

	_, err := m.ListFeaturedPackages(context.TODO())
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientListFeaturedPackagesCallLog(t *testing.T) {
	m := InitMockClient()

	if _, err := m.ListFeaturedPackages(context.TODO()); err != nil {
		t.Fatalf("ListFeaturedPackages: %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Method != "ListFeaturedPackages" {
		t.Fatalf("expected method ListFeaturedPackages, got %q", calls[0].Method)
	}
}
