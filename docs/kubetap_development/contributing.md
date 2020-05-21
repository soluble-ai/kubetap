# Contributing

## DCO

Contributions to kubetap require signing-off commits
to [accept the DCO](https://developercertificate.org/).

```txt
This is my commit message

Signed-off-by: Random J Developer <random@developer.example.org>
```

This is done by using the `-s` flag for `git commit`, as in:

```sh
$ git commit -s -m 'This is my commit message'
```

## Git Philosophy

* The `master` branch should be safe to deploy at any time, with the understanding
that while safe, it is less tested than a tagged release.
* Merge commits are ugly and not necessary.
* All commits should be squashed with an appropriate commit message prior to merge
into master.
* All commits to the `master` branch should be cryptographically signed.
* `git config --global pull.rebase = true`
