package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
)

type IntegrationTest struct {
	t *testing.T
}

func (test *IntegrationTest) ensureThereAreWorkflows(ctx context.Context, directory string) error {
	entries, err := os.ReadDir(directory + ".github/workflows/")

	if err != nil {
		return fmt.Errorf("cannot read dir %s: %w", directory, err)
	}

	foundWorkflows := false
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".yml") || strings.HasSuffix(entry.Name(), ".yaml") {
			foundWorkflows = true
			break
		}
	}

	if !assert.True(test.t, foundWorkflows) {
		return fmt.Errorf("no workflows found in directory %s", directory)
	}

	return nil
}

func (test *IntegrationTest) runCommand(ctx context.Context, command string) error {
	err := os.Chdir("../../")
	if err != nil {
		return err
	}
	defer func() {
		err := os.Chdir("test/integration")
		if err != nil {
			fmt.Println("could not reset directory")
		}
	}()

	cmd := exec.Command("ls", "test/integration")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("command \"%s\" produced an error: %w", command, err)
	}

	fmt.Printf("command output is\n%s", out)

	cmd = exec.Command("dagger", "run-zizmor-auto-fix", "--source=test/integration/testdata/zizmor-plain/")
	out, err = cmd.Output()
	if err != nil {
		return fmt.Errorf("command \"%s\" produced an error: %w", command, err)
	}

	fmt.Printf("command output is\n%s", out)

	return nil
}

func (test *IntegrationTest) fileIsProduced(ctx context.Context, file string) error {
	cmd := exec.Command("ls")
	out, err := cmd.Output()
	if err != nil {
		return err
	}

	fmt.Println(string(out))

	return nil
}

func TestFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: (&IntegrationTest{t: t}).InitializeScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features"},
			TestingT: t,
		},
	}

	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run feature tests")
	}
}

func (test *IntegrationTest) InitializeScenario(ctx *godog.ScenarioContext) {

	/*ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("failed to check directory of current execution: %w", err)
		}

		currDir := filepath.Base(filepath.Dir(exe))
		if currDir != "dagger" {
			return nil, fmt.Errorf("integration tests cannot be run from %s, must be run from dagger dir", currDir)
		}

		return context.Background(), nil
	})*/

	ctx.Given(`there are workflows in the repo at "(([a-zA-Z0-9_-]+/)+)"`, test.ensureThereAreWorkflows)
	ctx.When(`I run the command "(.+)"`, test.runCommand)
	ctx.Then(`a file named "([a-zA-Z_-]+\.out)" is produced`, test.fileIsProduced)
}
