# Contributing

This document describes the process of contributing to Unikraft CLI.
If you are considering contributing to this project, first of all, thank you!
The document is intended for anyone considering opening an **issue**, **discussion** or **pull request**.
For people who are interested in developing the CLI and technical details behind it, please check out our ["Development Guide"](HACKING.md) document as well.


## Code contribution guidelines

To make the contribution process as seamless as possible, please follow these requirements:

* Fork the project and make your changes.
* When you’re ready to create a pull request, be sure to:
  * Run `make lint` to check all documentation.
  * Squash your commits into to logical, [atomic commits](https://en.wikipedia.org/wiki/Atomic_commit) (`git rebase -i`).
    It's okay to force update your pull request with `git push -f`.
  * Follow the **Git Commit Message Guidelines** below.
* All commits must be signed off (`git commit -s`) by all authors in order to certify that the contributions are published under the [Developer Certificate of Origin (DCO)](https://wiki.linuxfoundation.org/dco).

## Git commit message guidelines

This project follows a modified version of the [AngularJS Commit Guidelines](https://github.com/angular/angular.js/blob/master/CONTRIBUTING.md#-git-commit-guidelines).
A commit message should take the following form:

```
<type>: <subject>
<BLANK LINE>
<body>
<BLANK LINE>
<footer>
```

where `<body>` and `<footer>` containing at least the contributions DCO.
The `<type>` should be one of the following:

- `chore`: Maintenance tasks for the repository itself.
- `ci`: Changes related to GitHub actions or any other CI/CD-related operations.
- `docs`: Documentation only changes (for example, this README, or source comments).
- `feat`: A new feature.
- `fix`: A bug fix.
- `perf`: A code change that improves performance (in this case, please include relevant benchmarks).
- `refactor`: A code change that neither fixes a bug nor adds a feature.
- `style`: Changes that don't affect the meaning of the code (for example, code formatting).
- `test`: Adding new tests, missing tests, or correcting existing tests.

An example would be something like:

```text
feat(guides): Add foobar deployment example

This commit adds a new example deployment to Unikraft Cloud
which demonstrates how to use foobar.

GitHub-Fixes: #30
Signed-off-by: Bobo Monkey <monkey@unikraft.com>
```
