## What this changes

<!-- One or two sentences. What is different after this is merged? -->

## Why

<!-- The problem it solves. If it changes an existing decision, say what that
     decision missed. -->

## How it was verified

<!-- Commands you ran, and anything you checked by hand. -->

```
make check
```

## Checklist

- [ ] `make check` passes (lint, govulncheck, tests with the race detector)
- [ ] New behaviour has a test
- [ ] README and command help match the binary after this change
- [ ] Commits follow Conventional Commits (`feat:`, `fix:`, `docs:`, …)
- [ ] An archive written before this change still restores after it, and
      `age -d … | zstd -d | tar -x` still works
- [ ] No secret, real archive or private path appears in the diff
