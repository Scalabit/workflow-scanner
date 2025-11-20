Feature: run zizmor
  As a workflow-scanner user
  I need to run zizmor with dagger on my repository
  And then obtain the report from zizmor

  Scenario: Run zizmor with plain format and autofix set to true
    Given there are workflows in the repo at "testdata/zizmor-plain/"
    When I run the command "dagger run-zizmor-auto-fix --source=test/integration/testdata/zizmor-plain/"
    Then a file named "zizmor_autofix.out" is produced