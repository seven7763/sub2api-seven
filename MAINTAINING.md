# Maintaining this fork

`seven7763/sub2api-seven` is a long-lived fork of
[`Wei-Shaw/sub2api`](https://github.com/Wei-Shaw/sub2api). It carries a
small number of local patches that are not yet merged upstream. This
document is the recipe for keeping the fork sane across upstream
releases.

## Branch layout

| Branch / tag | What it tracks |
|---|---|
| `main` | A clean mirror of `upstream/main`. **No local edits.** Only fast-forwarded from upstream. |
| `patches/vX.Y.Z` | Upstream tag `vX.Y.Z` + this fork's local patches. One branch per release we deploy. The newest one is what production runs. |
| `vX.Y.Z-seven.N` (tag) | Optional release tag stamped after a successful build (e.g. `v0.1.126-seven.1`). |

The deployed Docker image tag follows the branch:
`weishaw/sub2api:0.1.126-patched` (or `seven7763/sub2api:0.1.126-seven.1`
once we push to a registry of our own).

## Local patches in this fork

Each commit on a `patches/*` branch is a single, focused change so it
is easy to cherry-pick onto a future upstream release.

| Commit subject | Why |
|---|---|
| `fix(apicompat): accept array form of function_call_output.output` | `ResponsesInputItem.Output` was typed `string`, but the OpenAI Responses spec also accepts an array of typed parts. Strict clients (e.g. some OpenAI SDK paths sending image/text tool results) tripped `cannot unmarshal array into Go struct field …Output of type string` and got HTTP 422. |
| `feat(channels): collapse supported-models when more than 12` | The windsurf channel exposes ~78 models; the available-channels page rendered them all inline, blowing one row up to ~9 wrapped lines and pushing the rest of the table off the visible screen. Default-collapsed with a `+N more` toggle. |
| `build(docker): pin pnpm to 9.15.4` | `corepack prepare pnpm@latest` now resolves to v10.x, which hard-fails on `ERR_PNPM_IGNORED_BUILDS` (esbuild / vue-demi). Pinning v9 keeps the upstream Dockerfile untouched in spirit. |

When upstream finally lands an equivalent fix, drop the corresponding
commit from the next `patches/*` branch.

## Routine: sync `main` from upstream

```bash
cd /path/to/sub2api
git checkout main
git pull --ff-only upstream main
git push origin main
```

This must always succeed as a fast-forward. If it doesn't, someone
accidentally committed to `main`; rewrite history (`git reset --hard
upstream/main` + force-push) before doing anything else.

## Routine: rebase patches onto a new upstream release

When upstream tags `vX.Y.Z`:

```bash
# 1. Make sure tags are fetched.
git fetch upstream --tags

# 2. Inspect what changed since the last patched release.
git log --oneline patches/v<previous>..vX.Y.Z

# 3. Branch off the new tag.
git checkout -b patches/vX.Y.Z vX.Y.Z

# 4. Cherry-pick each fork commit, in order. The numbered list above
#    matches the desired order (apicompat → channels → docker).
git cherry-pick <hash-of-fix-apicompat>
git cherry-pick <hash-of-feat-channels>
git cherry-pick <hash-of-build-docker>
# Resolve any conflict, then `git cherry-pick --continue`.

# 5. Re-run tests and a build to confirm the patches still apply
#    cleanly to the new upstream code.
cd backend
go test ./internal/pkg/apicompat/...
go build ./...
cd ..

# 6. Push the branch.
git push -u origin patches/vX.Y.Z
```

## Routine: rebuild and redeploy the Docker image

The deploy host (`152.53.242.77`) keeps a working tree at
`/opt/sub2api-src-patched/`. To roll out a new patched build:

```bash
# On the deploy host:
cd /opt/sub2api-src-patched
git fetch origin
git checkout patches/vX.Y.Z

docker build \
  -t weishaw/sub2api:X.Y.Z-patched \
  --build-arg VERSION=X.Y.Z-patched \
  --build-arg COMMIT="$(git rev-parse --short HEAD)" \
  .

# Update docker-compose to point at the new tag.
sed -i "s|image: weishaw/sub2api:.*|image: weishaw/sub2api:X.Y.Z-patched|" \
  /opt/sub2api-deploy/docker-compose.yml

# Restart only the sub2api container; postgres/redis stay up.
cd /opt/sub2api-deploy
/usr/local/bin/docker-compose up -d --force-recreate sub2api

# Health probe.
curl -fsS http://127.0.0.1:8080/health
```

## Routine: emergency rollback

```bash
# On the deploy host:
sed -i "s|image: weishaw/sub2api:.*|image: weishaw/sub2api:latest|" \
  /opt/sub2api-deploy/docker-compose.yml
cd /opt/sub2api-deploy
/usr/local/bin/docker-compose up -d --force-recreate sub2api
```

The pre-patch `latest` image is kept locally on the host; a
`docker-compose.yml.bak.<timestamp>` is dropped by every patched
deploy in case the in-place sed edit needs to be unwound.

## Submitting patches upstream

Where applicable, open a PR against `Wei-Shaw/sub2api` for each commit
on `patches/*`. The commits in this fork are purposely scoped and
self-explanatory so they translate directly. Once a patch is merged
upstream and shipped in a tag, drop it from the next `patches/*`
branch on this fork.
