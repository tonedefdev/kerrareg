package utils

import (
	"fmt"

	"golang.org/x/mod/semver"

	kerraregv1alpha1 "github.com/tonedefdev/kerrareg/api/v1alpha1"
)

// getLatestVersion returns the latest semantic version of a Module
func GetLatestVersion(module kerraregv1alpha1.Module) *string {
	versions := make([]string, 0, len(module.Spec.Versions))
	for _, version := range module.Spec.Versions {
		var semverString string
		if version.Version[0] != 'v' {
			semverString = fmt.Sprintf("v%s", version.Version)
		} else {
			semverString = version.Version
		}
		semverString = semver.Canonical(semverString)
		versions = append(versions, semverString)
	}

	semver.Sort(versions)
	latestVersion := versions[len(versions)-1]
	return &latestVersion
}

// VersionsToKeep returns a slice of version strings that should be kept based on the version history limit specified in
// the Module or Provider's configuration.
// If the version history limit is nil or less than or equal to 0, nil is returned to indicate that all versions should be kept.
// If neither a Module nor a Provider is provided, an error is returned.
func VersionsToKeep(module *kerraregv1alpha1.Module, provider *kerraregv1alpha1.Provider) ([]string, error) {
	if module != nil {
		if module.Spec.ModuleConfig.VersionHistoryLimit == nil || *module.Spec.ModuleConfig.VersionHistoryLimit <= 0 {
			return nil, nil
		}
		versionHistoryLimit := *module.Spec.ModuleConfig.VersionHistoryLimit

		versions := make([]string, 0, len(module.Spec.Versions))
		for _, version := range module.Spec.Versions {
			var semverString string
			if version.Version[0] != 'v' {
				semverString = fmt.Sprintf("v%s", version.Version)
			} else {
				semverString = version.Version
			}
			semverString = semver.Canonical(semverString)
			versions = append(versions, semverString)
		}

		semver.Sort(versions)
		return versions[len(versions)-versionHistoryLimit:], nil
	}

	if provider != nil {
		if provider.Spec.ProviderConfig.VersionHistoryLimit == nil || *provider.Spec.ProviderConfig.VersionHistoryLimit <= 0 {
			return nil, nil
		}
		versionHistoryLimit := *provider.Spec.ProviderConfig.VersionHistoryLimit

		versions := make([]string, 0, len(provider.Spec.Versions))
		for _, version := range provider.Spec.Versions {
			var semverString string
			if version.Version[0] != 'v' {
				semverString = fmt.Sprintf("v%s", version.Version)
			} else {
				semverString = version.Version
			}
			semverString = semver.Canonical(semverString)
			versions = append(versions, semverString)
		}

		semver.Sort(versions)
		return versions[len(versions)-versionHistoryLimit:], nil
	}

	return nil, fmt.Errorf("both module and provider were nil, cannot determine versions")
}

// GetName returns the Module or Provider name as the resource's name if
// the configuration field for Config.Name is nil.
func GetName(module *kerraregv1alpha1.Module, provider *kerraregv1alpha1.Provider) (*string, error) {
	if module != nil {
		var moduleName string
		if module.Spec.ModuleConfig.Name != nil {
			moduleName = *module.Spec.ModuleConfig.Name
		} else {
			moduleName = module.Name
		}

		return &moduleName, nil
	}

	if provider != nil {
		var providerName string
		if provider.Spec.ProviderConfig.Name != nil {
			providerName = *provider.Spec.ProviderConfig.Name
		} else {
			providerName = provider.Name
		}

		return &providerName, nil
	}

	return nil, fmt.Errorf("both module and provider were nil, cannot determine name")
}

// GetVersionName returns the Module or Provider name as either the name of the object or
// from the specific resource's configuration field if it's non-nil.
// If neither a Module nor a Provider is provided, the function returns an error.
func GetVersionName(module *kerraregv1alpha1.Module, provider *kerraregv1alpha1.Provider, sanitizedModuleVersion string) (string, error) {
	if module != nil {
		var moduleVersionName string
		if module.Spec.ModuleConfig.Name == nil {
			moduleVersionName = fmt.Sprintf("%s-%s", module.Name, sanitizedModuleVersion)
			return moduleVersionName, nil
		}

		moduleVersionName = fmt.Sprintf("%s-%s", *module.Spec.ModuleConfig.Name, sanitizedModuleVersion)
		return moduleVersionName, nil
	}

	if provider != nil {
		var providerVersionName string
		if provider.Spec.ProviderConfig.Name == nil {
			providerVersionName = fmt.Sprintf("%s-%s", provider.Name, sanitizedModuleVersion)
			return providerVersionName, nil
		}

		providerVersionName = fmt.Sprintf("%s-%s", *provider.Spec.ProviderConfig.Name, sanitizedModuleVersion)
		return providerVersionName, nil
	}

	return "", fmt.Errorf("both module and provider were nil, cannot determine version name")
}

// SanitizeVersion removes leading 'v' from version strings for terraform/tofu version compatibility.
func SanitizeVersion(version string) string {
	if len(version) > 0 && version[0] == 'v' {
		version = version[1:]
	}
	return version
}
