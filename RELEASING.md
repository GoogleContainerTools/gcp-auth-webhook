# Releasing a new gcp-auth-webhook image
Bump the `VERSION` variable in the Makefile and open a PR to update it. Make sure `KO_VERSION` is the latest version according to its [github repo](https://github.com/google/ko/releases).
Once merged, create a tag with the same name as the new version, then create a releasein the github UI. Once the release is created, run `make image`. This will use `ko` to create the new multiarch image and push it to `$REGISTRY/gcp-auth-webhook` according to the REGISTRY variable in the Makefile.
