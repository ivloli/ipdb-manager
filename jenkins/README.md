# Jenkins

This folder contains all Jenkins-related files for `ipdb-manager`.

## Files

- `../Jenkinsfile`: main pipeline for release / rollback.
- `rollback_artifact_choices.groovy`: dynamic rollback artifact dropdown.

## How it is used

- `Jenkinsfile` handles checkout, config injection from Jenkins, package build, JFrog upload, rollback download, and target deployment.
- `rollback_artifact_choices.groovy` is used by the `ROLLBACK_ARTIFACT` parameter to fetch recent artifacts from Artifactory.

## Config flow

For `Release`:

1. Jenkins receives `CONFIG_YAML`.
2. Pipeline writes it to a temp file.
3. `make release-package CONFIG_SRC=<temp file>` builds the offline package.
4. The resulting tarball is uploaded to JFrog and deployed.

For `Rollback`:

1. Jenkins chooses an existing artifact from the dropdown.
2. Pipeline downloads it from JFrog.
3. The same deploy flow is used on target hosts.
