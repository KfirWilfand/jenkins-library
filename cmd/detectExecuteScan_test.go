package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	bd "github.com/SAP/jenkins-library/pkg/blackduck"
	"github.com/SAP/jenkins-library/pkg/format"
	piperGithub "github.com/SAP/jenkins-library/pkg/github"
	piperhttp "github.com/SAP/jenkins-library/pkg/http"
	"github.com/SAP/jenkins-library/pkg/mock"

	"github.com/google/go-github/v45/github"
	"github.com/stretchr/testify/assert"
)

type detectTestUtilsBundle struct {
	expectedError   error
	downloadedFiles map[string]string // src, dest
	*mock.ShellMockRunner
	*mock.FilesMock
	customEnv []string
}

func (d *detectTestUtilsBundle) GetIssueService() *github.IssuesService {
	return nil
}

func (d *detectTestUtilsBundle) GetSearchService() *github.SearchService {
	return nil
}

type httpMockClient struct {
	responseBodyForURL map[string]string
	errorMessageForURL map[string]string
	header             map[string]http.Header
}

func (c *httpMockClient) SetOptions(opts piperhttp.ClientOptions) {}
func (c *httpMockClient) SendRequest(method, url string, body io.Reader, header http.Header, cookies []*http.Cookie) (*http.Response, error) {
	c.header[url] = header
	response := http.Response{
		StatusCode: 200,
		Body:       ioutil.NopCloser(bytes.NewReader([]byte(""))),
	}

	if c.errorMessageForURL[url] != "" {
		response.StatusCode = 400
		return &response, fmt.Errorf(c.errorMessageForURL[url])
	}

	if c.responseBodyForURL[url] != "" {
		response.Body = ioutil.NopCloser(bytes.NewReader([]byte(c.responseBodyForURL[url])))
		return &response, nil
	}

	return &response, nil
}

func newBlackduckMockSystem(config detectExecuteScanOptions) blackduckSystem {
	myTestClient := httpMockClient{
		responseBodyForURL: map[string]string{
			"https://my.blackduck.system/api/tokens/authenticate":                                                                               authContent,
			"https://my.blackduck.system/api/projects?q=name%3ASHC-PiperTest":                                                                   projectContent,
			"https://my.blackduck.system/api/projects/5ca86e11-1983-4e7b-97d4-eb1a0aeffbbf/versions?limit=100&offset=0":                         projectVersionContent,
			"https://my.blackduck.system/api/projects/5ca86e11/versions/a6c94786/components?limit=999&offset=0":                                 componentsContent,
			"https://my.blackduck.system/api/projects/5ca86e11/versions/a6c94786/vunlerable-bom-components?limit=999&offset=0":                  vulnerabilitiesContent,
			"https://my.blackduck.system/api/projects/5ca86e11/versions/a6c94786/components?filter=policyCategory%3Alicense&limit=999&offset=0": componentsContent,
			"https://my.blackduck.system/api/projects/5ca86e11/versions/a6c94786/hierarchical-components?limit=999&offset=0":                    hierarchicalComponentsContent,
			"https://my.blackduck.system/api/projects/5ca86e11/versions/a6c94786/policy-status":                                                 policyStatusContent,
		},
		header: map[string]http.Header{},
	}
	sys := blackduckSystem{
		Client: bd.NewClient(config.Token, config.ServerURL, &myTestClient),
	}
	return sys
}

const (
	authContent = `{
		"bearerToken":"bearerTestToken",
		"expiresInMilliseconds":7199997
	}`
	projectContent = `{
		"totalCount": 1,
		"items": [
			{
				"name": "SHC-PiperTest",
				"_meta": {
					"href": "https://my.blackduck.system/api/projects/5ca86e11-1983-4e7b-97d4-eb1a0aeffbbf",
					"links": [
						{
							"rel": "versions",
							"href": "https://my.blackduck.system/api/projects/5ca86e11-1983-4e7b-97d4-eb1a0aeffbbf/versions"
						}
					]
				}
			}
		]
	}`
	projectVersionContent = `{
		"totalCount": 1,
		"items": [
			{
				"versionName": "1.0",
				"_meta": {
					"href": "https://my.blackduck.system/api/projects/5ca86e11-1983-4e7b-97d4-eb1a0aeffbbf/versions/a6c94786-0ee6-414f-9054-90d549c69c36",
					"links": [
						{
							"rel": "components",
							"href": "https://my.blackduck.system/api/projects/5ca86e11/versions/a6c94786/components"
						},
						{
							"rel": "hierarchical-components",
							"href": "https://my.blackduck.system/api/projects/5ca86e11/versions/a6c94786/hierarchical-components"
						},
						{
							"rel": "vulnerable-components",
							"href": "https://my.blackduck.system/api/projects/5ca86e11/versions/a6c94786/vunlerable-bom-components"
						},
						{
							"rel": "policy-status",
							"href": "https://my.blackduck.system/api/projects/5ca86e11/versions/a6c94786/policy-status"
						}
					]
				}
			}
		]
	}`
	componentsContent = `{
		"totalCount": 3,
		"items" : [
			{
				"componentName": "Spring Framework",
				"componentVersionName": "5.3.9",
				"primaryLanguage": "JAVA",
				"policyStatus": "IN_VIOLATION"
			}, {
				"componentName": "Apache Tomcat",
				"componentVersionName": "9.0.52",
				"primaryLanguage": "JAVA",
				"policyStatus": "IN_VIOLATION"
			}, {
				"componentName": "Apache Log4j",
				"componentVersionName": "4.5.16",
				"policyStatus": "UNKNOWN"
			}
		]
	}`
	hierarchicalComponentsContent = `{
		"totalCount": 3,
		"items" : [
			{
				"componentName": "Spring Framework",
				"componentVersionName": "5.3.9",
				"origins": [
					{
						"externalNamespace": "Maven", 
						"externalId": "spring:spring-web:5.3.9"
					}
				],
				"policyStatus": "IN_VIOLATION"
			}, {
				"componentName": "Apache Tomcat",
				"componentVersionName": "9.0.52",
				"origins": [
					{
						"externalNamespace": "Maven",
						"externalId": "apache:tomcat:9.0.52"
					}
				],
				"policyStatus": "IN_VIOLATION"
			}, {
				"componentName": "Apache Log4j",
				"componentVersionName": "4.5.16",
				"origins": [
					{
						"externalNamespace": "Maven",
						"externalId": "apache:log4j:4.5.16"
					}
				],
				"policyStatus": "UNKNOWN"
			}
		]
	}`
	vulnerabilitiesContent = `{
		"totalCount": 3,
		"items": [
			{
				"componentName": "Spring Framework",
				"componentVersionName": "5.3.9",
				"vulnerabilityWithRemediation" : {
					"vulnerabilityName" : "BDSA-2019-2021",
					"baseScore" : 7.5,
					"overallScore" : 7.5,
					"severity" : "HIGH",
					"remediationStatus" : "IGNORED",
					"description" : "description"
				}
			}, {
				"componentName": "Apache Log4j",
				"componentVersionName": "4.5.16",
				"vulnerabilityWithRemediation" : {
					"vulnerabilityName" : "BDSA-2020-4711",
					"baseScore" : 7.5,
					"overallScore" : 7.5,
					"severity" : "HIGH",
					"remediationStatus" : "IGNORED",
					"description" : "description"
				}
			}, {
				"componentName": "Apache Log4j",
				"componentVersionName": "4.5.16",
				"vulnerabilityWithRemediation" : {
					"vulnerabilityName" : "BDSA-2020-4712",
					"baseScore" : 4.5,
					"overallScore" : 4.5,
					"severity" : "MEDIUM",
					"remediationStatus" : "IGNORED",
					"description" : "description"
				}
			}
		]
	}`
	policyStatusContent = `{
		"overallStatus": "IN_VIOLATION",
		"componentVersionPolicyViolationDetails": {
			"name": "IN_VIOLATION",
			"severityLevels": [{"name":"BLOCKER", "value": 1}, {"name": "CRITICAL", "value": 1}]
		}
	}`
)

func (c *detectTestUtilsBundle) RunExecutable(string, ...string) error {
	panic("not expected to be called in test")
}

func (c *detectTestUtilsBundle) SetOptions(piperhttp.ClientOptions) {
}

func (c *detectTestUtilsBundle) GetOsEnv() []string {
	return c.customEnv
}

func (c *detectTestUtilsBundle) DownloadFile(url, filename string, _ http.Header, _ []*http.Cookie) error {
	if c.expectedError != nil {
		return c.expectedError
	}

	if c.downloadedFiles == nil {
		c.downloadedFiles = make(map[string]string)
	}
	c.downloadedFiles[url] = filename
	return nil
}

func (w *detectTestUtilsBundle) CreateIssue(ghCreateIssueOptions *piperGithub.CreateIssueOptions) error {
	return nil
}

func newDetectTestUtilsBundle() *detectTestUtilsBundle {
	utilsBundle := detectTestUtilsBundle{
		ShellMockRunner: &mock.ShellMockRunner{},
		FilesMock:       &mock.FilesMock{},
	}
	return &utilsBundle
}

func TestRunDetect(t *testing.T) {
	t.Parallel()
	t.Run("success case", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		utilsMock := newDetectTestUtilsBundle()
		utilsMock.AddFile("detect.sh", []byte(""))
		err := runDetect(ctx, detectExecuteScanOptions{}, utilsMock, &detectExecuteScanInflux{})

		assert.Equal(t, utilsMock.downloadedFiles["https://detect.synopsys.com/detect7.sh"], "detect.sh")
		assert.True(t, utilsMock.HasRemovedFile("detect.sh"))
		assert.NoError(t, err)
		assert.Equal(t, ".", utilsMock.Dir, "Wrong execution directory used")
		assert.Equal(t, "/bin/bash", utilsMock.Shell[0], "Bash shell expected")
		expectedScript := "./detect.sh --blackduck.url= --blackduck.api.token= \"--detect.project.name=''\" \"--detect.project.version.name=''\" \"--detect.code.location.name=''\" --detect.source.path='.'"
		assert.Equal(t, expectedScript, utilsMock.Calls[0])
	})

	t.Run("success case detect 6", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		utilsMock := newDetectTestUtilsBundle()
		utilsMock.AddFile("detect.sh", []byte(""))
		options := detectExecuteScanOptions{
			CustomEnvironmentVariables: []string{"DETECT_LATEST_RELEASE_VERSION=6.8.0"},
		}
		err := runDetect(ctx, options, utilsMock, &detectExecuteScanInflux{})

		assert.Equal(t, utilsMock.downloadedFiles["https://detect.synopsys.com/detect.sh"], "detect.sh")
		assert.True(t, utilsMock.HasRemovedFile("detect.sh"))
		assert.NoError(t, err)
		assert.Equal(t, ".", utilsMock.Dir, "Wrong execution directory used")
		assert.Equal(t, "/bin/bash", utilsMock.Shell[0], "Bash shell expected")
		expectedScript := "./detect.sh --blackduck.url= --blackduck.api.token= \"--detect.project.name=''\" \"--detect.project.version.name=''\" \"--detect.code.location.name=''\" --detect.source.path='.'"
		assert.Equal(t, expectedScript, utilsMock.Calls[0])
	})

	t.Run("success case detect 6 from OS env", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		utilsMock := newDetectTestUtilsBundle()
		utilsMock.AddFile("detect.sh", []byte(""))
		utilsMock.customEnv = []string{"DETECT_LATEST_RELEASE_VERSION=6.8.0"}
		err := runDetect(ctx, detectExecuteScanOptions{}, utilsMock, &detectExecuteScanInflux{})

		assert.Equal(t, utilsMock.downloadedFiles["https://detect.synopsys.com/detect.sh"], "detect.sh")
		assert.True(t, utilsMock.HasRemovedFile("detect.sh"))
		assert.NoError(t, err)
		assert.Equal(t, ".", utilsMock.Dir, "Wrong execution directory used")
		assert.Equal(t, "/bin/bash", utilsMock.Shell[0], "Bash shell expected")
		expectedScript := "./detect.sh --blackduck.url= --blackduck.api.token= \"--detect.project.name=''\" \"--detect.project.version.name=''\" \"--detect.code.location.name=''\" --detect.source.path='.'"
		assert.Equal(t, expectedScript, utilsMock.Calls[0])
	})

	t.Run("failure case", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		utilsMock := newDetectTestUtilsBundle()
		utilsMock.ShouldFailOnCommand = map[string]error{"./detect.sh --blackduck.url= --blackduck.api.token= \"--detect.project.name=''\" \"--detect.project.version.name=''\" \"--detect.code.location.name=''\" --detect.source.path='.'": fmt.Errorf("")}
		utilsMock.ExitCode = 3
		utilsMock.AddFile("detect.sh", []byte(""))
		err := runDetect(ctx, detectExecuteScanOptions{FailOnSevereVulnerabilities: true}, utilsMock, &detectExecuteScanInflux{})
		assert.Equal(t, utilsMock.ExitCode, 3)
		assert.Contains(t, err.Error(), "FAILURE_POLICY_VIOLATION => Detect found policy violations.")
		assert.True(t, utilsMock.HasRemovedFile("detect.sh"))
	})

	t.Run("maven parameters", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		utilsMock := newDetectTestUtilsBundle()
		utilsMock.CurrentDir = "root_folder"
		utilsMock.AddFile("detect.sh", []byte(""))
		err := runDetect(ctx, detectExecuteScanOptions{
			M2Path:              ".pipeline/local_repo",
			ProjectSettingsFile: "project-settings.xml",
			GlobalSettingsFile:  "global-settings.xml",
		}, utilsMock, &detectExecuteScanInflux{})

		assert.NoError(t, err)
		assert.Equal(t, ".", utilsMock.Dir, "Wrong execution directory used")
		assert.Equal(t, "/bin/bash", utilsMock.Shell[0], "Bash shell expected")
		absoluteLocalPath := string(os.PathSeparator) + filepath.Join("root_folder", ".pipeline", "local_repo")

		expectedParam := "\"--detect.maven.build.command='--global-settings global-settings.xml --settings project-settings.xml -Dmaven.repo.local=" + absoluteLocalPath + "'\""
		assert.Contains(t, utilsMock.Calls[0], expectedParam)
	})
}

func TestAddDetectArgs(t *testing.T) {
	t.Parallel()
	testData := []struct {
		args     []string
		options  detectExecuteScanOptions
		expected []string
	}{
		{
			args: []string{"--testProp1=1"},
			options: detectExecuteScanOptions{
				ScanProperties:  []string{"--scan1=1", "--scan2=2"},
				ServerURL:       "https://server.url",
				Token:           "apiToken",
				ProjectName:     "testName",
				Version:         "1.0",
				VersioningModel: "major-minor",
				CodeLocation:    "",
				Scanners:        []string{"signature"},
				ScanPaths:       []string{"path1", "path2"},
			},
			expected: []string{
				"--testProp1=1",
				"--scan1=1",
				"--scan2=2",
				"--blackduck.url=https://server.url",
				"--blackduck.api.token=apiToken",
				"\"--detect.project.name='testName'\"",
				"\"--detect.project.version.name='1.0'\"",
				"\"--detect.code.location.name='testName/1.0'\"",
				"--detect.blackduck.signature.scanner.paths=path1,path2",
				"--detect.source.path='.'",
			},
		},
		{
			args: []string{"--testProp1=1"},
			options: detectExecuteScanOptions{
				ServerURL:       "https://server.url",
				Token:           "apiToken",
				ProjectName:     "testName",
				Version:         "1.0",
				VersioningModel: "major-minor",
				CodeLocation:    "testLocation",
				FailOn:          []string{"BLOCKER", "MAJOR"},
				Scanners:        []string{"source"},
				ScanPaths:       []string{"path1", "path2"},
				Groups:          []string{"testGroup"},
			},
			expected: []string{
				"--testProp1=1",
				"--blackduck.url=https://server.url",
				"--blackduck.api.token=apiToken",
				"\"--detect.project.name='testName'\"",
				"\"--detect.project.version.name='1.0'\"",
				"\"--detect.project.user.groups='testGroup'\"",
				"--detect.policy.check.fail.on.severities=BLOCKER,MAJOR",
				"\"--detect.code.location.name='testLocation'\"",
				"--detect.blackduck.signature.scanner.paths=path1,path2",
				"--detect.source.path='.'",
			},
		},
		{
			args: []string{"--testProp1=1"},
			options: detectExecuteScanOptions{
				ServerURL:       "https://server.url",
				Token:           "apiToken",
				ProjectName:     "testName",
				CodeLocation:    "testLocation",
				FailOn:          []string{"BLOCKER", "MAJOR"},
				Scanners:        []string{"source"},
				ScanPaths:       []string{"path1", "path2"},
				Groups:          []string{"testGroup", "testGroup2"},
				Version:         "1.0",
				VersioningModel: "major-minor",
			},
			expected: []string{
				"--testProp1=1",
				"--blackduck.url=https://server.url",
				"--blackduck.api.token=apiToken",
				"\"--detect.project.name='testName'\"",
				"\"--detect.project.version.name='1.0'\"",
				"\"--detect.project.user.groups='testGroup,testGroup2'\"",
				"--detect.policy.check.fail.on.severities=BLOCKER,MAJOR",
				"\"--detect.code.location.name='testLocation'\"",
				"--detect.blackduck.signature.scanner.paths=path1,path2",
				"--detect.source.path='.'",
			},
		},
		{
			args: []string{"--testProp1=1"},
			options: detectExecuteScanOptions{
				ServerURL:       "https://server.url",
				Token:           "apiToken",
				ProjectName:     "testName",
				CodeLocation:    "testLocation",
				FailOn:          []string{"BLOCKER", "MAJOR"},
				Scanners:        []string{"source"},
				ScanPaths:       []string{"path1", "path2"},
				Groups:          []string{"testGroup", "testGroup2"},
				Version:         "1.0",
				VersioningModel: "major-minor",
				DependencyPath:  "pathx",
			},
			expected: []string{
				"--testProp1=1",
				"--blackduck.url=https://server.url",
				"--blackduck.api.token=apiToken",
				"\"--detect.project.name='testName'\"",
				"\"--detect.project.version.name='1.0'\"",
				"\"--detect.project.user.groups='testGroup,testGroup2'\"",
				"--detect.policy.check.fail.on.severities=BLOCKER,MAJOR",
				"\"--detect.code.location.name='testLocation'\"",
				"--detect.blackduck.signature.scanner.paths=path1,path2",
				"--detect.source.path=pathx",
			},
		},
		{
			args: []string{"--testProp1=1"},
			options: detectExecuteScanOptions{
				ServerURL:       "https://server.url",
				Token:           "apiToken",
				ProjectName:     "testName",
				CodeLocation:    "testLocation",
				FailOn:          []string{"BLOCKER", "MAJOR"},
				Scanners:        []string{"source"},
				ScanPaths:       []string{"path1", "path2"},
				Groups:          []string{"testGroup", "testGroup2"},
				Version:         "1.0",
				VersioningModel: "major-minor",
				DependencyPath:  "pathx",
				Unmap:           true,
			},
			expected: []string{
				"--testProp1=1",
				"--detect.project.codelocation.unmap=true",
				"--blackduck.url=https://server.url",
				"--blackduck.api.token=apiToken",
				"\"--detect.project.name='testName'\"",
				"\"--detect.project.version.name='1.0'\"",
				"\"--detect.project.user.groups='testGroup,testGroup2'\"",
				"--detect.policy.check.fail.on.severities=BLOCKER,MAJOR",
				"\"--detect.code.location.name='testLocation'\"",
				"--detect.blackduck.signature.scanner.paths=path1,path2",
				"--detect.source.path=pathx",
			},
		},
		{
			args: []string{"--testProp1=1"},
			options: detectExecuteScanOptions{
				ServerURL:               "https://server.url",
				Token:                   "apiToken",
				ProjectName:             "testName",
				CodeLocation:            "testLocation",
				FailOn:                  []string{"BLOCKER", "MAJOR"},
				Scanners:                []string{"source"},
				ScanPaths:               []string{"path1", "path2"},
				Groups:                  []string{"testGroup", "testGroup2"},
				Version:                 "1.0",
				VersioningModel:         "major-minor",
				DependencyPath:          "pathx",
				Unmap:                   true,
				IncludedPackageManagers: []string{"maven", "GRADLE"},
				ExcludedPackageManagers: []string{"npm", "NUGET"},
				MavenExcludedScopes:     []string{"TEST", "compile"},
				DetectTools:             []string{"DETECTOR"},
			},
			expected: []string{
				"--testProp1=1",
				"--detect.project.codelocation.unmap=true",
				"--blackduck.url=https://server.url",
				"--blackduck.api.token=apiToken",
				"\"--detect.project.name='testName'\"",
				"\"--detect.project.version.name='1.0'\"",
				"\"--detect.project.user.groups='testGroup,testGroup2'\"",
				"--detect.policy.check.fail.on.severities=BLOCKER,MAJOR",
				"\"--detect.code.location.name='testLocation'\"",
				"--detect.blackduck.signature.scanner.paths=path1,path2",
				"--detect.source.path=pathx",
				"--detect.included.detector.types=MAVEN,GRADLE",
				"--detect.excluded.detector.types=NPM,NUGET",
				"--detect.maven.excluded.scopes=test,compile",
				"--detect.tools=DETECTOR",
			},
		},
		{
			args: []string{"--testProp1=1"},
			options: detectExecuteScanOptions{
				ServerURL:               "https://server.url",
				Token:                   "apiToken",
				ProjectName:             "testName",
				CodeLocation:            "testLocation",
				FailOn:                  []string{"BLOCKER", "MAJOR"},
				Scanners:                []string{"source"},
				ScanPaths:               []string{"path1", "path2"},
				Groups:                  []string{"testGroup", "testGroup2"},
				Version:                 "1.0",
				VersioningModel:         "major-minor",
				DependencyPath:          "pathx",
				Unmap:                   true,
				IncludedPackageManagers: []string{"maven", "GRADLE"},
				ExcludedPackageManagers: []string{"npm", "NUGET"},
				MavenExcludedScopes:     []string{"TEST", "compile"},
				DetectTools:             []string{"DETECTOR"},
				ScanOnChanges:           true,
			},
			expected: []string{
				"--testProp1=1",
				"--report",
				"--blackduck.url=https://server.url",
				"--blackduck.api.token=apiToken",
				"\"--detect.project.name='testName'\"",
				"\"--detect.project.version.name='1.0'\"",
				"\"--detect.project.user.groups='testGroup,testGroup2'\"",
				"--detect.policy.check.fail.on.severities=BLOCKER,MAJOR",
				"\"--detect.code.location.name='testLocation'\"",
				"--detect.blackduck.signature.scanner.paths=path1,path2",
				"--detect.source.path=pathx",
				"--detect.included.detector.types=MAVEN,GRADLE",
				"--detect.excluded.detector.types=NPM,NUGET",
				"--detect.maven.excluded.scopes=test,compile",
				"--detect.tools=DETECTOR",
			},
		},
		{
			args: []string{"--testProp1=1"},
			options: detectExecuteScanOptions{
				ServerURL:               "https://server.url",
				Token:                   "apiToken",
				ProjectName:             "testName",
				CodeLocation:            "testLocation",
				FailOn:                  []string{"BLOCKER", "MAJOR"},
				Scanners:                []string{"source"},
				ScanPaths:               []string{"path1", "path2"},
				Groups:                  []string{"testGroup", "testGroup2"},
				Version:                 "1.0",
				VersioningModel:         "major-minor",
				DependencyPath:          "pathx",
				Unmap:                   true,
				IncludedPackageManagers: []string{"maven", "GRADLE"},
				ExcludedPackageManagers: []string{"npm", "NUGET"},
				MavenExcludedScopes:     []string{"TEST", "compile"},
				DetectTools:             []string{"DETECTOR"},
				ScanOnChanges:           true,
			},
			expected: []string{
				"--testProp1=1",
				"--report",
				"--blackduck.url=https://server.url",
				"--blackduck.api.token=apiToken",
				"\"--detect.project.name='testName'\"",
				"\"--detect.project.version.name='1.0'\"",
				"\"--detect.project.user.groups='testGroup,testGroup2'\"",
				"--detect.policy.check.fail.on.severities=BLOCKER,MAJOR",
				"\"--detect.code.location.name='testLocation'\"",
				"--detect.blackduck.signature.scanner.paths=path1,path2",
				"--detect.source.path=pathx",
				"--detect.included.detector.types=MAVEN,GRADLE",
				"--detect.excluded.detector.types=NPM,NUGET",
				"--detect.maven.excluded.scopes=test,compile",
				"--detect.tools=DETECTOR",
			},
		},
		{
			args: []string{"--testProp1=1"},
			options: detectExecuteScanOptions{
				ScanProperties:          []string{"--scan=1", "--detect.project.codelocation.unmap=true"},
				ServerURL:               "https://server.url",
				Token:                   "apiToken",
				ProjectName:             "testName",
				CodeLocation:            "testLocation",
				FailOn:                  []string{"BLOCKER", "MAJOR"},
				Scanners:                []string{"source"},
				ScanPaths:               []string{"path1", "path2"},
				Groups:                  []string{"testGroup", "testGroup2"},
				Version:                 "1.0",
				VersioningModel:         "major-minor",
				DependencyPath:          "pathx",
				Unmap:                   true,
				IncludedPackageManagers: []string{"maven", "GRADLE"},
				ExcludedPackageManagers: []string{"npm", "NUGET"},
				MavenExcludedScopes:     []string{"TEST", "compile"},
				DetectTools:             []string{"DETECTOR"},
				ScanOnChanges:           true,
			},
			expected: []string{
				"--testProp1=1",
				"--report",
				"--scan=1",
				"--blackduck.url=https://server.url",
				"--blackduck.api.token=apiToken",
				"\"--detect.project.name='testName'\"",
				"\"--detect.project.version.name='1.0'\"",
				"\"--detect.project.user.groups='testGroup,testGroup2'\"",
				"--detect.policy.check.fail.on.severities=BLOCKER,MAJOR",
				"\"--detect.code.location.name='testLocation'\"",
				"--detect.blackduck.signature.scanner.paths=path1,path2",
				"--detect.source.path=pathx",
				"--detect.included.detector.types=MAVEN,GRADLE",
				"--detect.excluded.detector.types=NPM,NUGET",
				"--detect.maven.excluded.scopes=test,compile",
				"--detect.tools=DETECTOR",
			},
		},
		{
			args: []string{"--testProp1=1"},
			options: detectExecuteScanOptions{
				ServerURL:       "https://server.url",
				Token:           "apiToken",
				ProjectName:     "testName",
				Version:         "1.0",
				VersioningModel: "major-minor",
				CodeLocation:    "",
				ScanPaths:       []string{"path1", "path2"},
				MinScanInterval: 4,
			},
			expected: []string{
				"--testProp1=1",
				"--detect.blackduck.signature.scanner.arguments='--min-scan-interval=4'",
				"--blackduck.url=https://server.url",
				"--blackduck.api.token=apiToken",
				"\"--detect.project.name='testName'\"",
				"\"--detect.project.version.name='1.0'\"",
				"\"--detect.code.location.name='testName/1.0'\"",
				"--detect.blackduck.signature.scanner.paths=path1,path2",
				"--detect.source.path='.'",
			},
		},
	}

	for k, v := range testData {
		v := v
		t.Run(fmt.Sprintf("run %v", k), func(t *testing.T) {
			t.Parallel()
			got, err := addDetectArgs(v.args, v.options, newDetectTestUtilsBundle())
			assert.NoError(t, err)
			assert.Equal(t, v.expected, got)
		})
	}
}

// Testing exit code mapping method
func TestExitCodeMapping(t *testing.T) {
	cases := []struct {
		exitCode int
		expected string
	}{
		{1, "FAILURE_BLACKDUCK_CONNECTIVITY"},
		{-1, "Not known exit code key"},
		{8, "Not known exit code key"},
		{100, "FAILURE_UNKNOWN_ERROR"},
	}

	for _, c := range cases {
		response := exitCodeMapping(c.exitCode)
		assert.Contains(t, response, c.expected)
	}
}

func TestPostScanChecksAndReporting(t *testing.T) {
	t.Parallel()
	t.Run("Reporting after scan", func(t *testing.T) {
		ctx := context.Background()
		config := detectExecuteScanOptions{Token: "token", ServerURL: "https://my.blackduck.system", ProjectName: "SHC-PiperTest", Version: "", CustomScanVersion: "1.0"}
		utils := newDetectTestUtilsBundle()
		sys := newBlackduckMockSystem(config)
		err := postScanChecksAndReporting(ctx, config, &detectExecuteScanInflux{}, utils, &sys)

		assert.EqualError(t, err, "License Policy Violations found")
		content, err := utils.FileRead("blackduck-ip.json")
		assert.NoError(t, err)
		assert.Contains(t, string(content), `"policyViolations":2`)
	})
}

func TestIsMajorVulnerability(t *testing.T) {
	t.Parallel()
	t.Run("Case True", func(t *testing.T) {
		vr := bd.VulnerabilityWithRemediation{
			OverallScore: 7.5,
			Severity:     "HIGH",
		}
		v := bd.Vulnerability{
			Name:                         "",
			VulnerabilityWithRemediation: vr,
			Ignored:                      false,
		}
		assert.True(t, isMajorVulnerability(v))
	})
	t.Run("Case Ignored Components", func(t *testing.T) {
		vr := bd.VulnerabilityWithRemediation{
			OverallScore: 7.5,
			Severity:     "HIGH",
		}
		v := bd.Vulnerability{
			Name:                         "",
			VulnerabilityWithRemediation: vr,
			Ignored:                      true,
		}
		assert.False(t, isMajorVulnerability(v))
	})
	t.Run("Case False", func(t *testing.T) {
		vr := bd.VulnerabilityWithRemediation{
			OverallScore: 7.5,
			Severity:     "MEDIUM",
		}
		v := bd.Vulnerability{
			Name:                         "",
			VulnerabilityWithRemediation: vr,
			Ignored:                      false,
		}
		assert.False(t, isMajorVulnerability(v))
	})
}

func TestIsActiveVulnerability(t *testing.T) {
	t.Parallel()
	t.Run("Case true", func(t *testing.T) {
		vr := bd.VulnerabilityWithRemediation{
			OverallScore:      7.5,
			Severity:          "HIGH",
			RemediationStatus: "NEW",
		}
		v := bd.Vulnerability{
			Name:                         "",
			VulnerabilityWithRemediation: vr,
		}
		assert.True(t, isActiveVulnerability(v))
	})
	t.Run("Case False", func(t *testing.T) {
		vr := bd.VulnerabilityWithRemediation{
			OverallScore:      7.5,
			Severity:          "HIGH",
			RemediationStatus: "IGNORED",
		}
		v := bd.Vulnerability{
			Name:                         "",
			VulnerabilityWithRemediation: vr,
		}
		assert.False(t, isActiveVulnerability(v))
	})
}

func TestIsActivePolicyViolation(t *testing.T) {
	t.Parallel()
	t.Run("Case true", func(t *testing.T) {
		assert.True(t, isActivePolicyViolation("IN_VIOLATION"))
	})
	t.Run("Case False", func(t *testing.T) {
		assert.False(t, isActivePolicyViolation("NOT_IN_VIOLATION"))
	})
}

func TestGetActivePolicyViolations(t *testing.T) {
	t.Parallel()
	t.Run("Case true", func(t *testing.T) {
		config := detectExecuteScanOptions{Token: "token", ServerURL: "https://my.blackduck.system", ProjectName: "SHC-PiperTest", Version: "", CustomScanVersion: "1.0"}
		sys := newBlackduckMockSystem(config)

		components, err := sys.Client.GetComponents("SHC-PiperTest", "1.0")
		assert.NoError(t, err)
		assert.Equal(t, 2, getActivePolicyViolations(components))
	})
}

func TestGetVulnsAndComponents(t *testing.T) {
	t.Parallel()
	t.Run("Case true", func(t *testing.T) {
		config := detectExecuteScanOptions{Token: "token", ServerURL: "https://my.blackduck.system", ProjectName: "SHC-PiperTest", Version: "", CustomScanVersion: "1.0"}
		sys := newBlackduckMockSystem(config)

		vulns, assessedVulns, components, err := getVulnsAndComponents(config, &[]format.Assessment{}, &detectExecuteScanInflux{}, &sys)
		assert.NoError(t, err)
		assert.Equal(t, 3, len(vulns.Items))
		assert.Equal(t, 3, len(components.Items))
		assert.Equal(t, 0, len(assessedVulns.Items))
		assert.Equal(t, 0, assessedVulns.TotalCount)
		vulnerabilitySpring := bd.Vulnerability{}
		vulnerabilityLog4j1 := bd.Vulnerability{}
		vulnerabilityLog4j2 := bd.Vulnerability{}
		for _, v := range vulns.Items {
			if v.VulnerabilityWithRemediation.VulnerabilityName == "BDSA-2019-2021" {
				vulnerabilitySpring = v
			}
			if v.VulnerabilityWithRemediation.VulnerabilityName == "BDSA-2020-4711" {
				vulnerabilityLog4j1 = v
			}
			if v.VulnerabilityWithRemediation.VulnerabilityName == "BDSA-2020-4712" {
				vulnerabilityLog4j2 = v
			}
		}
		vulnerableComponentSpring := bd.HierarchicalComponent{}
		vulnerableComponentLog4j := bd.HierarchicalComponent{}
		for _, c := range components.Items {
			if c.Name == "Spring Framework" {
				vulnerableComponentSpring = c
			}
			if c.Name == "Apache Log4j" {
				vulnerableComponentLog4j = c
			}
		}
		assert.Equal(t, vulnerableComponentSpring, *vulnerabilitySpring.Component)
		assert.Equal(t, vulnerableComponentLog4j, *vulnerabilityLog4j1.Component)
		assert.Equal(t, vulnerableComponentLog4j, *vulnerabilityLog4j2.Component)
	})
}

func TestFilterAssessedVulnerabilities(t *testing.T) {
	t.Run("success case", func(t *testing.T) {
		config := detectExecuteScanOptions{BuildTool: "maven", Token: "token", ServerURL: "https://my.blackduck.system", ProjectName: "SHC-PiperTest", Version: "", CustomScanVersion: "1.0"}
		sys := newBlackduckMockSystem(config)

		vulnerabilities, err := sys.Client.GetVulnerabilities(config.ProjectName, getVersionName(config))
		assert.NoError(t, err, "unexpected error when loading vulnerabilities")
		components, err := sys.Client.GetHierarchicalComponents(config.ProjectName, getVersionName(config))
		keyFormat := "%v/%v"
		componentLookup := map[string]bd.HierarchicalComponent{}
		for _, comp := range components.Items {
			componentLookup[fmt.Sprintf(keyFormat, comp.Name, comp.Version)] = comp
		}
		assert.NoError(t, err, "unexpected error when loading components")

		assert.Equal(t, 3, vulnerabilities.TotalCount)
		assert.Equal(t, 3, len(vulnerabilities.Items))

		assessments := []format.Assessment{}
		assessment := format.Assessment{}
		assessment.Analysis = format.RiskAccepted
		assessment.Vulnerability = "BDSA-2019-2021"
		assessment.Purls = []format.Purl{
			{
				Purl: "pkg:maven/spring/spring-web@5.3.9",
			},
		}
		assessments = append(assessments, assessment)
		unassessedVulns, assessedVulns := filterAssessedVulnerabilities(vulnerabilities, &assessments, keyFormat, componentLookup)
		assert.Equal(t, 2, unassessedVulns.TotalCount)
		assert.Equal(t, 2, len(unassessedVulns.Items))
		assert.Equal(t, 1, assessedVulns.TotalCount)
		assert.Equal(t, 1, len(assessedVulns.Items))
		assert.Equal(t, "BDSA-2019-2021", assessedVulns.Items[0].VulnerabilityWithRemediation.VulnerabilityName)
	})
}
