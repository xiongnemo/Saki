# Repository Instructions

## Go Modules

Use the China-friendly Go module proxy for dependency resolution:

```pwsh
$env:GOPROXY = 'https://goproxy.cn,direct'
```

For one-off commands:

```pwsh
$env:GOPROXY='https://goproxy.cn,direct'; go mod tidy
```

## Project TODOs

Check `TODO.md` before changing release packaging, Windows SMTC integration, or GitHub Actions builds.

Record your plan to `TODO.md` before implementation to avoid conflicts with other contributors. This helps maintain a clear roadmap and prevents duplicate efforts. Update `TODO.md` with your progress and any changes to the plan (and mark whether it's completed).

## Documentation and Commits

After completing a feature, check whether `README.md` needs to be updated.

After completing a feature or fixing a bug, commit the changes you wrote.

## Versioning

Build and release versions use this format:

```text
v{major}.{minor}.{patch}-{branch}-{commit12}[-dirty]
```

When release packaging or build scripts compute versions automatically, derive
`patch` from the nearest exact semver tag (`vMAJOR.MINOR.PATCH`) plus the
number of commits since that tag. Ignore prerelease/dev tags such as
`v0.0.1-dev-...` for this calculation.

To bump major/minor or reset the patch base, create and push a new exact semver
tag on the desired base commit, for example `v0.1.0`. The tagged commit builds
as `v0.1.0-...`; the next commit builds as `v0.1.1-...`.

Only set `SAKI_BASE_VERSION` or `BASE_VERSION` when intentionally overriding the
automatic Git-derived base version.
