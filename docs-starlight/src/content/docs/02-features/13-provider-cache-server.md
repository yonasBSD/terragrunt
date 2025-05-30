---
title: Provider Cache Server
description: Learn how to use the Terragrunt provider cache server.
slug: docs/features/provider-cache-server
sidebar:
  order: 13
---

Terragrunt has the ability to cache OpenTofu/Terraform providers across all OpenTofu/Terraform runs. The Provider Cache Server feature ensures that each provider is only ever downloaded and stored on disk exactly once by running a local provider cache server while Terragrunt runs OpenTofu/Terraform commands.

The Provider Cache Server is a performance optimization. For more details on performance optimizations, their tradeoffs, and other performance tips, read the dedicated [Performance documentation](/docs/troubleshooting/performance).

## Why caching is useful

Let's imagine that your project consists of 50 Terragrunt units, and each of them uses the same `aws` provider. Without caching, each of them will download the provider from the Internet, and store it in its own `.terraform` directory. For clarity, the downloadable archive `terraform-provider-aws_5.36.0_darwin_arm64.zip` has a size of ~100MB, and when unzipped it takes up ~450MB of disk space. It’s easy to calculate that initializing such a project with 50 modules will cost you 5GB of traffic and 22.5GB of free space instead of 100MB and 450MB using the cache.

## Why OpenTofu/Terraform's built-in provider caching doesn't work

OpenTofu/Terraform has a provider caching feature, the [Provider Plugin Cache](https://opentofu.org/docs/cli/config/config-file/#provider-plugin-cache), that does the job well... unless you run multiple OpenTofu/Terraform processes simultaneously, such as when you use `terragrunt run --all`. Then the OpenTofu/Terraform processes begin conflict by overwriting each other’s cache, which causes an error such as `Error: Failed to install provider`. As a result, Terragrunt previously had to disable concurrency for `init` steps in `run --all`, which is significantly slower. If you enable Terragrunt Provider Caching, as described in this section, that will no longer be necessary, and you should see significant performance improvements with `init`, as well as significant savings in terms of bandwidth and disk space usage.

## Usage

Terragrunt Provider Cache is currently considered an experimental feature, so it is disabled by default. To enable it, you need to use the flag [`provider-cache`](https://terragrunt.gruntwork.io/docs/reference/cli/commands/run#provider-cache):

```shell
terragrunt run --all apply --provider-cache
```

or the environment variable `TG_PROVIDER_CACHE`:

```shell
TG_PROVIDER_CACHE=1 terragrunt run --all apply
```

By default, cached providers are stored in `terragrunt/providers` folder, which is located in the user cache directory:

- `$HOME/.terragrunt-cache/terragrunt/providers` on Unix systems
- `$HOME/Library/Caches/terragrunt/providers` on Darwin
- `%LocalAppData%\terragrunt\providers` on Windows

The file structure of the cache directory is identical to the OpenTofu/Terraform [plugin_cache_dir](https://opentofu.org/docs/cli/config/config-file/#provider-plugin-cache) directory. If you already have a directory with providers cached by OpenTofu/Terraform [plugin_cache_dir](https://opentofu.org/docs/cli/config/config-file/#provider-plugin-cache), you can set this path using the flag [`provider-cache-dir`](/docs/reference/cli/commands/run#provider-cache-dir), to enable the Provider Cache Server to reuse existing cached providers.

```shell
terragrunt plan \
--provider-cache \
--provider-cache-dir /new/path/to/cache/dir
```

or the environment variable `TG_PROVIDER_CACHE_DIR`:

```shell
TG_PROVIDER_CACHE=1 \
TG_PROVIDER_CACHE_DIR=/new/path/to/cache/dir \
terragrunt plan
```

By default, Terragrunt only caches providers from the following registries: `registry.terraform.io`, `registry.opentofu.org`. You can override this list using the flag [`provider-cache-registry-names`](https://terragrunt.gruntwork.io/docs/reference/cli/commands/run#provider-cache-registry-names):

```shell
terragrunt apply \
--provider-cache \
--provider-cache-registry-names example1.com \
--provider-cache-registry-names example2.com
```

or the environment variable `TG_PROVIDER_CACHE_REGISTRY_NAMES`:

```shell
TG_PROVIDER_CACHE=1 \
TG_PROVIDER_CACHE_REGISTRY_NAMES=example1.com,example2.com \
terragrunt apply
```

## How Terragrunt Provider Caching works

- Start a server on localhost. This is the _Terragrunt Provider Cache server_.
- Configure OpenTofu/Terraform instances to use the Terragrunt Provider Cache server as a remote registry:

  - Create local CLI config file `.terraformrc` for each module that concatenates the user configuration from the OpenTofu/Terraform [CLI config file](https://opentofu.org/docs/cli/config/config-file/) with additional sections:

  - [provider-installation](https://opentofu.org/docs/cli/config/config-file/#provider-installation) forces OpenTofu/Terraform to look for the required providers in the cache directory and create symbolic links to them, if not found, then request them from the remote registry.
  - [host](https://github.com/hashicorp/terraform/issues/28309) forces OpenTofu/Terraform to [forward](#how-forwarding-requests-through-the-provider-cache-server-works) all provider requests through the Terragrunt Provider Cache server. The address link contains [UUID](https://en.wikipedia.org/wiki/Universally_unique_identifier) and is unique for each module, used by Terragrunt Provider Cache server to associate modules with the requested providers.
  - Set environment variables:
    - [TF_CLI_CONFIG_FILE](https://opentofu.org/docs/cli/config/environment-variables/#tf_plugin_cache_dir) sets to use just created local CLI config `.terragrunt-cache/.terraformrc`
    - [TF*TOKEN*\*](https://opentofu.org/docs/cli/config/config-file/#environment-variable-credentials) sets per-remote-registry tokens for authentication to Terragrunt Provider Cache server.

- Any time Terragrunt is going to run `init`:
  - Call `tofu/terraform init`. This gets OpenTofu/Terraform to request all the providers it needs from the Terragrunt Provider Cache server.
  - The Terragrunt Provider Cache server will download the provider from the remote registry, unpack and store it into the cache directory or [create a symlink](#reusing-providers-from-the-user-plugins-directory) if the required provider exists in the user plugins directory. Note that the Terragrunt Provider Cache server will ensure that each unique provider is only ever downloaded and stored on disk once, handling concurrency (from multiple OpenTofu/Terraform and Terragrunt instances) correctly. Along with the provider, the cache server downloads hashes and signatures of the providers to check that the files are not corrupted.
  - The Terragrunt Provider Cache server returns the HTTP status _429 Locked_ to OpenTofu/Terraform. This is because we do _not_ want OpenTofu/Terraform to actually download any providers as a result of calling `tofu/terraform init`; we only use that command to request the Terragrunt Provider Cache Server to start caching providers.
  - At this point, all providers are downloaded and cached, so finally, we run `terragrunt init` a second time, which will find all the providers it needs in the cache, and it'll create symlinks to them nearly instantly, with no additional downloading.
  - Note that if a OpenTofu/Terraform module doesn't have a lock file, OpenTofu/Terraform does _not_ use the cache, so it would end up downloading all the providers from scratch. To work around this, we generate `.terraform.lock.hcl` based on the request made by `tofu/terraform init` to the Terragrunt Provider Cache server. Since `terraform init` only requests the providers that need to be added/updated, we can keep track of them using the Terragrunt Provider Cache server and update the OpenTofu/Terraform lock file with the appropriate hashes without having to parse `tf` configs.

### Reusing providers from the user plugins directory

Some plugins for some operating systems may not be available in the remote registries. Thus, the cache server will not be able to download the requested provider. As an example, plugin `template v2.2.0` for `darwin-arm64`, see [Template v2.2.0 does not have a package available - Mac M1](https://discuss.hashicorp.com/t/template-v2-2-0-does-not-have-a-package-available-mac-m1/35099). The workaround is to compile the plugin from source code and put it into the user plugins directory or use the automated solution [https://github.com/kreuzwerker/m1-terraform-provider-helper](https://github.com/kreuzwerker/m1-terraform-provider-helper). For this reason, the cache server first tries to create a symlink from the user's plugin directory if the required provider already exists there:

- %APPDATA%\terraform.d\plugins on Windows
- ~/.terraform.d/plugins on other systems

### How forwarding requests through the Provider Cache Server works

OpenTofu/Terraform has an official documented setting [network_mirror](https://developer.hashicorp.com/terraform/cli/config/config-file#network_mirror), that works great, but has one major drawback for the local cache server - the need to use an HTTPS connection with a trusted certificate. Fortunately, there is another way - using the undocumented [host](https://github.com/hashicorp/terraform/issues/28309) setting, which allows OpenTofu/Terraform to create connections to the caching server over HTTP.

### Provider Cache with `providers lock` command

If you run `providers lock` with enabled Terragrunt Provider Cache, Terragrunt creates the provider cache and generates the lock file on its own, without running `terraform providers lock` at all.

```shell
terragrunt providers lock -platform=linux_amd64 -platform=darwin_arm64 -platform=freebsd_amd64 \
--provider-cache
```

## Configure the Provider Cache Server

Since the Provider Cache Server is essentially a Private Registry server that accepts requests from OpenTofu/Terraform, downloads and saves providers to the cache directory, there are a few more flags that are unlikely to be needed, but are useful to know about:

- [`provider-cache-hostname`](https://terragrunt.gruntwork.io/docs/reference/cli/commands/run#provider-cache-hostname) - Default: `localhost`.
- [`provider-cache-port`](https://terragrunt.gruntwork.io/docs/reference/cli/commands/run#provider-cache-port) - Default: Assigned random port automatically.
- [`provider-cache-token`](https://terragrunt.gruntwork.io/docs/reference/cli/commands/run#provider-cache-token) - Default: Generated randomly.

To enhance security, the Terragrunt Provider Cache has authentication to prevent unauthorized connections from third-party applications. You can set your own token using any character set.

```shell
terragrunt apply \
--provider-cache \
--provider-cache-host 192.168.0.100 \
--provider-cache-port 5758 \
--provider-cache-token my-secret
```

or using environment variables:

```shell
TG_PROVIDER_CACHE=1 \
TG_PROVIDER_CACHE_HOST=192.168.0.100 \
TG_PROVIDER_CACHE_PORT=5758 \
TG_PROVIDER_CACHE_TOKEN=my-secret \
terragrunt apply
```
