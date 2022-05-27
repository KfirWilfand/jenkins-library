package kubernetes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/SAP/jenkins-library/pkg/docker"
	"github.com/SAP/jenkins-library/pkg/log"
	"github.com/pkg/errors"
	"helm.sh/helm/v3/pkg/cli/values"
)

type KubernetesDeploy interface {
	RunHelmDeploy() error
	RunKubectlDeploy() error
}

type KubernetesDeployBundle struct {
	config  KubernetesOptions
	utils   DeployUtils
	verbose bool
	stdout  io.Writer
}

func NewKubernetesDeploy(config KubernetesOptions, utils DeployUtils, verbose bool, stdout io.Writer) KubernetesDeploy {
	return &KubernetesDeployBundle{
		config:  config,
		utils:   utils,
		verbose: verbose,
		stdout:  stdout,
	}
}

type KubernetesOptions struct {
	AdditionalParameters       []string               `json:"additionalParameters,omitempty"`
	APIServer                  string                 `json:"apiServer,omitempty"`
	AppTemplate                string                 `json:"appTemplate,omitempty"`
	ChartPath                  string                 `json:"chartPath,omitempty"`
	ContainerRegistryPassword  string                 `json:"containerRegistryPassword,omitempty"`
	ContainerImageName         string                 `json:"containerImageName,omitempty"`
	ContainerImageTag          string                 `json:"containerImageTag,omitempty"`
	ContainerRegistryURL       string                 `json:"containerRegistryUrl,omitempty"`
	ContainerRegistryUser      string                 `json:"containerRegistryUser,omitempty"`
	ContainerRegistrySecret    string                 `json:"containerRegistrySecret,omitempty"`
	CreateDockerRegistrySecret bool                   `json:"createDockerRegistrySecret,omitempty"`
	DeploymentName             string                 `json:"deploymentName,omitempty"`
	DeployTool                 string                 `json:"deployTool,omitempty" validate:"possible-values=kubectl helm helm3"`
	ForceUpdates               bool                   `json:"forceUpdates,omitempty"`
	HelmDeployWaitSeconds      int                    `json:"helmDeployWaitSeconds,omitempty"`
	HelmValues                 []string               `json:"helmValues,omitempty"`
	ValuesMapping              map[string]interface{} `json:"valuesMapping,omitempty"`
	Image                      string                 `json:"image,omitempty"`
	ImageNames                 []string               `json:"imageNames,omitempty"`
	ImageNameTags              []string               `json:"imageNameTags,omitempty"`
	ImageDigests               []string               `json:"imageDigests,omitempty"`
	IngressHosts               []string               `json:"ingressHosts,omitempty"`
	KeepFailedDeployments      bool                   `json:"keepFailedDeployments,omitempty"`
	RunHelmTests               bool                   `json:"runHelmTests,omitempty"`
	ShowTestLogs               bool                   `json:"showTestLogs,omitempty"`
	KubeConfig                 string                 `json:"kubeConfig,omitempty"`
	KubeContext                string                 `json:"kubeContext,omitempty"`
	KubeToken                  string                 `json:"kubeToken,omitempty"`
	Namespace                  string                 `json:"namespace,omitempty"`
	TillerNamespace            string                 `json:"tillerNamespace,omitempty"`
	DockerConfigJSON           string                 `json:"dockerConfigJSON,omitempty"`
	DeployCommand              string                 `json:"deployCommand,omitempty" validate:"possible-values=apply replace"`
}

func (k *KubernetesDeployBundle) RunHelmDeploy() error {
	if len(k.config.ChartPath) <= 0 {
		return fmt.Errorf("chart path has not been set, please configure chartPath parameter")
	}
	if len(k.config.DeploymentName) <= 0 {
		return fmt.Errorf("deployment name has not been set, please configure deploymentName parameter")
	}
	_, containerRegistry, err := splitRegistryURL(k.config.ContainerRegistryURL)
	if err != nil {
		log.Entry().WithError(err).Fatalf("Container registry url '%v' incorrect", k.config.ContainerRegistryURL)
	}

	helmValues, err := defineDeploymentValues(k.config, containerRegistry)
	if err != nil {
		return errors.Wrap(err, "failed to process deployment values")
	}

	helmLogFields := map[string]interface{}{}
	helmLogFields["Chart Path"] = k.config.ChartPath
	helmLogFields["Namespace"] = k.config.Namespace
	helmLogFields["Deployment Name"] = k.config.DeploymentName
	helmLogFields["Context"] = k.config.KubeContext
	helmLogFields["Kubeconfig"] = k.config.KubeConfig
	log.Entry().WithFields(helmLogFields).Debug("Calling Helm")

	helmEnv := []string{fmt.Sprintf("KUBECONFIG=%v", k.config.KubeConfig)}
	if k.config.DeployTool == "helm" && len(k.config.TillerNamespace) > 0 {
		helmEnv = append(helmEnv, fmt.Sprintf("TILLER_NAMESPACE=%v", k.config.TillerNamespace))
	}
	log.Entry().Debugf("Helm SetEnv: %v", helmEnv)
	k.utils.SetEnv(helmEnv)
	k.utils.Stdout(k.stdout)

	if k.config.DeployTool == "helm" {
		initParams := []string{"init", "--client-only"}
		if err := k.utils.RunExecutable("helm", initParams...); err != nil {
			log.Entry().WithError(err).Fatal("Helm init call failed")
		}
	}

	if len(k.config.ContainerRegistryUser) == 0 && len(k.config.ContainerRegistryPassword) == 0 {
		log.Entry().Info("No/incomplete container registry credentials provided: skipping secret creation")
		if len(k.config.ContainerRegistrySecret) > 0 {
			helmValues.add("imagePullSecrets[0].name", k.config.ContainerRegistrySecret)
		}
	} else {
		var dockerRegistrySecret bytes.Buffer
		k.utils.Stdout(&dockerRegistrySecret)
		err, kubeSecretParams := defineKubeSecretParams(k.config, containerRegistry, k.utils)
		if err != nil {
			log.Entry().WithError(err).Fatal("parameter definition for creating registry secret failed")
		}
		log.Entry().Infof("Calling kubectl create secret --dry-run=true ...")
		log.Entry().Debugf("kubectl parameters %v", kubeSecretParams)
		if err := k.utils.RunExecutable("kubectl", kubeSecretParams...); err != nil {
			log.Entry().WithError(err).Fatal("Retrieving Docker config via kubectl failed")
		}

		var dockerRegistrySecretData struct {
			Kind string `json:"kind"`
			Data struct {
				DockerConfJSON string `json:".dockerconfigjson"`
			} `json:"data"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(dockerRegistrySecret.Bytes(), &dockerRegistrySecretData); err != nil {
			log.Entry().WithError(err).Fatal("Reading docker registry secret json failed")
		}
		// make sure that secret is hidden in log output
		log.RegisterSecret(dockerRegistrySecretData.Data.DockerConfJSON)

		log.Entry().Debugf("Secret created: %v", dockerRegistrySecret.String())

		// pass secret in helm default template way and in Piper backward compatible way
		helmValues.add("secret.name", k.config.ContainerRegistrySecret)
		helmValues.add("secret.dockerconfigjson", dockerRegistrySecretData.Data.DockerConfJSON)
		helmValues.add("imagePullSecrets[0].name", k.config.ContainerRegistrySecret)
	}

	// Deprecated functionality
	// only for backward compatible handling of ingress.hosts
	// this requires an adoption of the default ingress.yaml template
	// Due to the way helm is implemented it is currently not possible to overwrite a part of a list:
	// see: https://github.com/helm/helm/issues/5711#issuecomment-636177594
	// Recommended way is to use a custom values file which contains the appropriate data
	for i, h := range k.config.IngressHosts {
		helmValues.add(fmt.Sprintf("ingress.hosts[%v]", i), h)
	}

	upgradeParams := []string{
		"upgrade",
		k.config.DeploymentName,
		k.config.ChartPath,
	}

	for _, v := range k.config.HelmValues {
		upgradeParams = append(upgradeParams, "--values", v)
	}

	err = helmValues.mapValues()
	if err != nil {
		return errors.Wrap(err, "failed to map values using 'valuesMapping' configuration")
	}

	upgradeParams = append(
		upgradeParams,
		"--install",
		"--namespace", k.config.Namespace,
		"--set", strings.Join(helmValues.marshal(), ","),
	)

	if k.config.ForceUpdates {
		upgradeParams = append(upgradeParams, "--force")
	}

	if k.config.DeployTool == "helm" {
		upgradeParams = append(upgradeParams, "--wait", "--timeout", strconv.Itoa(k.config.HelmDeployWaitSeconds))
	}

	if k.config.DeployTool == "helm3" {
		upgradeParams = append(upgradeParams, "--wait", "--timeout", fmt.Sprintf("%vs", k.config.HelmDeployWaitSeconds))
	}

	if !k.config.KeepFailedDeployments {
		upgradeParams = append(upgradeParams, "--atomic")
	}

	if len(k.config.KubeContext) > 0 {
		upgradeParams = append(upgradeParams, "--kube-context", k.config.KubeContext)
	}

	if len(k.config.AdditionalParameters) > 0 {
		upgradeParams = append(upgradeParams, k.config.AdditionalParameters...)
	}

	k.utils.Stdout(k.stdout)
	log.Entry().Info("Calling helm upgrade ...")
	log.Entry().Debugf("Helm parameters %v", upgradeParams)
	if err := k.utils.RunExecutable("helm", upgradeParams...); err != nil {
		log.Entry().WithError(err).Fatal("Helm upgrade call failed")
	}

	testParams := []string{
		"test",
		k.config.DeploymentName,
		"--namespace", k.config.Namespace,
	}

	if k.config.ShowTestLogs {
		testParams = append(
			testParams,
			"--logs",
		)
	}

	if k.config.RunHelmTests {
		if err := k.utils.RunExecutable("helm", testParams...); err != nil {
			log.Entry().WithError(err).Fatal("Helm test call failed")
		}
	}

	return nil
}

func (k *KubernetesDeployBundle) RunKubectlDeploy() error {
	_, containerRegistry, err := splitRegistryURL(k.config.ContainerRegistryURL)
	if err != nil {
		log.Entry().WithError(err).Fatalf("Container registry url '%v' incorrect", k.config.ContainerRegistryURL)
	}

	kubeParams := []string{
		"--insecure-skip-tls-verify=true",
		fmt.Sprintf("--namespace=%v", k.config.Namespace),
	}

	if len(k.config.KubeConfig) > 0 {
		log.Entry().Info("Using KUBECONFIG environment for authentication.")
		kubeEnv := []string{fmt.Sprintf("KUBECONFIG=%v", k.config.KubeConfig)}
		k.utils.SetEnv(kubeEnv)
		if len(k.config.KubeContext) > 0 {
			kubeParams = append(kubeParams, fmt.Sprintf("--context=%v", k.config.KubeContext))
		}

	} else {
		log.Entry().Info("Using --token parameter for authentication.")
		kubeParams = append(kubeParams, fmt.Sprintf("--server=%v", k.config.APIServer))
		kubeParams = append(kubeParams, fmt.Sprintf("--token=%v", k.config.KubeToken))
	}

	k.utils.Stdout(k.stdout)

	if len(k.config.ContainerRegistryUser) == 0 && len(k.config.ContainerRegistryPassword) == 0 {
		log.Entry().Info("No/incomplete container registry credentials provided: skipping secret creation")
	} else {
		err, kubeSecretParams := defineKubeSecretParams(k.config, containerRegistry, k.utils)
		if err != nil {
			log.Entry().WithError(err).Fatal("parameter definition for creating registry secret failed")
		}
		var dockerRegistrySecret bytes.Buffer
		k.utils.Stdout(&dockerRegistrySecret)
		log.Entry().Infof("Creating container registry secret '%v'", k.config.ContainerRegistrySecret)
		kubeSecretParams = append(kubeSecretParams, kubeParams...)
		log.Entry().Debugf("Running kubectl with following parameters: %v", kubeSecretParams)
		if err := k.utils.RunExecutable("kubectl", kubeSecretParams...); err != nil {
			log.Entry().WithError(err).Fatal("Creating container registry secret failed")
		}

		var dockerRegistrySecretData map[string]interface{}

		if err := json.Unmarshal(dockerRegistrySecret.Bytes(), &dockerRegistrySecretData); err != nil {
			log.Entry().WithError(err).Fatal("Reading docker registry secret json failed")
		}

		// write the json output to a file
		tmpFolder := getTempDirForKubeCtlJSON()
		defer os.RemoveAll(tmpFolder) // clean up
		jsonData, _ := json.Marshal(dockerRegistrySecretData)
		ioutil.WriteFile(filepath.Join(tmpFolder, "secret.json"), jsonData, 0777)

		kubeSecretApplyParams := []string{"apply", "-f", filepath.Join(tmpFolder, "secret.json")}
		if err := k.utils.RunExecutable("kubectl", kubeSecretApplyParams...); err != nil {
			log.Entry().WithError(err).Fatal("Creating container registry secret failed")
		}

	}

	appTemplate, err := k.utils.FileRead(k.config.AppTemplate)
	if err != nil {
		log.Entry().WithError(err).Fatalf("Error when reading appTemplate '%v'", k.config.AppTemplate)
	}

	values, err := defineDeploymentValues(k.config, containerRegistry)
	if err != nil {
		return errors.Wrap(err, "failed to process deployment values")
	}
	err = values.mapValues()
	if err != nil {
		return errors.Wrap(err, "failed to map values using 'valuesMapping' configuration")
	}

	re := regexp.MustCompile(`image:[ ]*<image-name>`)
	placeholderFound := re.Match(appTemplate)

	if placeholderFound {
		log.Entry().Warn("image placeholder '<image-name>' is deprecated and does not support multi-image replacement, please use Helm-like template syntax '{{ .Values.image.[image-name].reposotory }}:{{ .Values.image.[image-name].tag }}")
		if values.singleImage {
			// Update image name in deployment yaml, expects placeholder like 'image: <image-name>'
			appTemplate = []byte(re.ReplaceAllString(string(appTemplate), fmt.Sprintf("image: %s:%s", values.get("image.repository"), values.get("image.tag"))))
		} else {
			return fmt.Errorf("multi-image replacement not supported for single image placeholder")
		}
	}

	buf := bytes.NewBufferString("")
	tpl, err := template.New("appTemplate").Parse(string(appTemplate))
	if err != nil {
		return errors.Wrap(err, "failed to parse app-template file")
	}
	err = tpl.Execute(buf, values.asHelmValues())
	if err != nil {
		return errors.Wrap(err, "failed to render app-template file")
	}

	err = k.utils.FileWrite(k.config.AppTemplate, buf.Bytes(), 0700)
	if err != nil {
		return errors.Wrapf(err, "Error when updating appTemplate '%v'", k.config.AppTemplate)
	}

	kubeParams = append(kubeParams, k.config.DeployCommand, "--filename", k.config.AppTemplate)
	if k.config.ForceUpdates && k.config.DeployCommand == "replace" {
		kubeParams = append(kubeParams, "--force")
	}

	if len(k.config.AdditionalParameters) > 0 {
		kubeParams = append(kubeParams, k.config.AdditionalParameters...)
	}
	if err := k.utils.RunExecutable("kubectl", kubeParams...); err != nil {
		log.Entry().Debugf("Running kubectl with following parameters: %v", kubeParams)
		log.Entry().WithError(err).Fatal("Deployment with kubectl failed.")
	}
	return nil
}

type deploymentValues struct {
	mapping     map[string]interface{}
	singleImage bool
	values      []struct {
		key, value string
	}
}

func (dv *deploymentValues) add(key, value string) {
	dv.values = append(dv.values, struct {
		key   string
		value string
	}{
		key:   key,
		value: value,
	})
}

func (dv deploymentValues) get(key string) string {
	for _, item := range dv.values {
		if item.key == key {
			return item.value
		}
	}

	return ""
}

func (dv *deploymentValues) mapValues() error {
	var keys []string
	for k := range dv.mapping {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, dst := range keys {
		srcString, ok := dv.mapping[dst].(string)
		if !ok {
			return fmt.Errorf("invalid path '%#v' is used for valuesMapping, only strings are supported", dv.mapping[dst])
		}
		if val := dv.get(srcString); val != "" {
			dv.add(dst, val)
		} else {
			log.Entry().Warnf("can not map '%s: %s', %s is not set", dst, dv.mapping[dst], dv.mapping[dst])
		}
	}

	return nil
}

func (dv deploymentValues) marshal() []string {
	var result []string
	for _, item := range dv.values {
		result = append(result, fmt.Sprintf("%s=%s", item.key, item.value))
	}
	return result
}

func (dv *deploymentValues) asHelmValues() map[string]interface{} {
	valuesOpts := values.Options{
		Values: dv.marshal(),
	}
	mergedValues, err := valuesOpts.MergeValues(nil)
	if err != nil {
		log.Entry().WithError(err).Fatal("failed to process deployment values")
	}
	return map[string]interface{}{
		"Values": mergedValues,
	}
}

func joinKey(parts ...string) string {
	escapedParts := make([]string, 0, len(parts))
	replacer := strings.NewReplacer(".", "_", "-", "_")
	for _, part := range parts {
		escapedParts = append(escapedParts, replacer.Replace(part))
	}
	return strings.Join(escapedParts, ".")
}

func getTempDirForKubeCtlJSON() string {
	tmpFolder, err := ioutil.TempDir(".", "temp-")
	if err != nil {
		log.Entry().WithError(err).WithField("path", tmpFolder).Debug("creating temp directory failed")
	}
	return tmpFolder
}

func splitRegistryURL(registryURL string) (protocol, registry string, err error) {
	parts := strings.Split(registryURL, "://")
	if len(parts) != 2 || len(parts[1]) == 0 {
		return "", "", fmt.Errorf("Failed to split registry url '%v'", registryURL)
	}
	return parts[0], parts[1], nil
}

func splitFullImageName(image string) (imageName, tag string, err error) {
	parts := strings.Split(image, ":")
	switch len(parts) {
	case 0:
		return "", "", fmt.Errorf("Failed to split image name '%v'", image)
	case 1:
		if len(parts[0]) > 0 {
			return parts[0], "", nil
		}
		return "", "", fmt.Errorf("Failed to split image name '%v'", image)
	case 2:
		return parts[0], parts[1], nil
	}
	return "", "", fmt.Errorf("Failed to split image name '%v'", image)
}

func defineKubeSecretParams(config KubernetesOptions, containerRegistry string, utils DeployUtils) (error, []string) {
	targetPath := ""
	if len(config.DockerConfigJSON) > 0 {
		// first enhance config.json with additional pipeline-related credentials if they have been provided
		if len(containerRegistry) > 0 && len(config.ContainerRegistryUser) > 0 && len(config.ContainerRegistryPassword) > 0 {
			var err error
			targetPath, err = docker.CreateDockerConfigJSON(containerRegistry, config.ContainerRegistryUser, config.ContainerRegistryPassword, "", config.DockerConfigJSON, utils)
			if err != nil {
				log.Entry().Warningf("failed to update Docker config.json: %v", err)
				return err, []string{}
			}
		}

	} else {
		return fmt.Errorf("no docker config json file found to update credentials '%v'", config.DockerConfigJSON), []string{}
	}
	return nil, []string{
		"create",
		"secret",
		"generic",
		config.ContainerRegistrySecret,
		fmt.Sprintf("--from-file=.dockerconfigjson=%v", targetPath),
		"--type=kubernetes.io/dockerconfigjson",
		"--insecure-skip-tls-verify=true",
		"--dry-run=client",
		"--output=json",
	}
}

func defineDeploymentValues(config KubernetesOptions, containerRegistry string) (*deploymentValues, error) {
	var err error
	var useDigests bool
	dv := &deploymentValues{
		mapping: config.ValuesMapping,
	}
	if len(config.ImageNames) > 0 {
		if len(config.ImageNames) != len(config.ImageNameTags) {
			log.SetErrorCategory(log.ErrorConfiguration)
			return nil, fmt.Errorf("number of imageNames and imageNameTags must be equal")
		}
		if len(config.ImageDigests) > 0 {
			if len(config.ImageDigests) != len(config.ImageNameTags) {
				log.SetErrorCategory(log.ErrorConfiguration)
				return nil, fmt.Errorf("number of imageDigests and imageNameTags must be equal")
			}

			useDigests = true
		}
		for i, key := range config.ImageNames {
			name, tag, err := splitFullImageName(config.ImageNameTags[i])
			if err != nil {
				log.Entry().WithError(err).Fatalf("Container image '%v' incorrect", config.ImageNameTags[i])
			}

			if useDigests {
				tag = fmt.Sprintf("%s@%s", tag, config.ImageDigests[i])
			}

			dv.add(joinKey("image", key, "repository"), fmt.Sprintf("%v/%v", containerRegistry, name))
			dv.add(joinKey("image", key, "tag"), tag)

			if len(config.ImageNames) == 1 {
				dv.singleImage = true
				dv.add("image.repository", fmt.Sprintf("%v/%v", containerRegistry, name))
				dv.add("image.tag", tag)
			}
		}
	} else {
		// support either image or containerImageName and containerImageTag
		containerImageName := ""
		containerImageTag := ""
		dv.singleImage = true

		if len(config.Image) > 0 {
			containerImageName, containerImageTag, err = splitFullImageName(config.Image)
			if err != nil {
				log.Entry().WithError(err).Fatalf("Container image '%v' incorrect", config.Image)
			}
		} else if len(config.ContainerImageName) > 0 && len(config.ContainerImageTag) > 0 {
			containerImageName = config.ContainerImageName
			containerImageTag = config.ContainerImageTag
		} else {
			return nil, fmt.Errorf("image information not given - please either set image or containerImageName and containerImageTag")
		}
		dv.add("image.repository", fmt.Sprintf("%v/%v", containerRegistry, containerImageName))
		dv.add("image.tag", containerImageTag)

		dv.add(joinKey("image", containerImageName, "repository"), fmt.Sprintf("%v/%v", containerRegistry, containerImageName))
		dv.add(joinKey("image", containerImageName, "tag"), containerImageTag)
	}

	return dv, nil
}
