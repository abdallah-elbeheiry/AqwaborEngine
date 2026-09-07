# Releasing the engine

Go has no publish step and no registry.
A semver git tag pushed to GitHub *is* a released version: `proxy.golang.org` fetches it the first
time anyone asks for it, and from then on that version exists for everybody.

That makes the tag the whole release system, and everything below is process around that one act.

## Cutting a release

```
git checkout master
git pull
go build ./... && go test ./...
git tag -a v1.1.0 -m "what this release is"
git push origin v1.1.0
```

The push fires `.github/workflows/release.yml`, which re-runs the checks against the tagged commit,
writes the release notes from the commit subjects since the previous tag, and publishes a GitHub
release.

Nothing else has to happen. A consumer gets the new version with `go get`.

## A tag is immutable once it is published

The moment the proxy has served a version, it caches it permanently, and the checksum database
records what it contained.
Moving the tag afterwards does not give anyone the new code; it gives them a checksum mismatch and a
build that refuses to run.

So a bad release is fixed by a new version, never by a corrected tag.
If a release is actively harmful — it corrupts saves, it leaks something — add a `retract` block to
`go.mod` and release again, which is how the toolchain is told to skip it.

Tags that were never pushed are free to delete. Only publication is final.

## Version numbers

`vMAJOR.MINOR.PATCH`.

Patch is a fix that changes no API.
Minor is new API that existing code keeps compiling against.
Major is a break.

**A major bump changes the module path.** At v2 and above Go requires the path to carry the major as
a suffix, so `github.com/abdallah-elbeheiry/AqwaborEngine` becomes `.../AqwaborEngine/v2`, and every
import line in every consumer is rewritten. That is the real cost of a breaking change, and it is
why v1.0.0 is a promise rather than a milestone: it says the API is now something other people can
build on.

The `go` directive in `go.mod` is a floor on everyone who imports the engine.
Raising it breaks anyone on an older toolchain even though no code changed, so it belongs in the
release notes when it moves.

## Prereleases, and the game that is built alongside

The engine is authoritative over the games built on it: where it lacks something a game needs, the
engine gains it rather than the game working around it.
That makes it a moving dependency, and prereleases are how a moving dependency stays usable.

```
git tag -a v1.1.0-rc.1 -m "voltage on the power graph, for testing"
git push origin v1.1.0-rc.1
```

A version with a hyphen in it is a prerelease. `go get` never selects one on its own, so nobody is
upgraded into it by accident; a game pins it explicitly, plays against it, and reports back.
The plain `v1.1.0` tag follows once it has held up.

This is what replaces a local `go.work` pointing at a checkout. It does the thing a workspace cannot:
it gives the version a name that two people can say to each other.

## The workflows

`ci.yml` answers "is master broken" on every push and pull request: gofmt, vet, build, test.
It is the gate that matters, because a tag cut off a broken master is the one mistake the release
process cannot undo.

`release.yml` runs only on `v*` tags. It repeats those checks against the tagged commit rather than
trusting the branch run, publishes the release, and asks the module proxy for the new version so the
first consumer is not the one waiting for it to be fetched.

Release notes are built from `git log` subjects, which is why the commit convention already in use
here — `feat:`, `fix:`, `refactor:` — is worth keeping. GitHub's own generated notes list merged
pull requests only, and would come out empty on a repository where work lands as direct pushes.
