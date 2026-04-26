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
