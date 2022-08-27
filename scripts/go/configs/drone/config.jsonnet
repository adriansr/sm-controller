local settings = import 'settings.libsonnet';

local vault_secret(name, vault_path, key) = {
  kind: 'secret',
  name: name,
  get: {
    path: vault_path,
    name: key,
  },
};

local step(name, commands, dependencies=[], image='golang:' + settings.go_version) = {
  name: name,
  commands: commands,
  image: image,
  depends_on: dependencies,
};

local pipeline(name, steps=[]) = {
  kind: 'pipeline',
  type: 'docker',
  name: name,
  image_pull_secrets: [ 'docker_config_json' ],
  volumes: [
    {
      name: 'docker',
      host: {
        path: '/var/run/docker.sock',
      },
    },
    {
      name: 'config',
      temp: {},
    },
  ],
  steps: [ step('runner identification', [ 'echo $DRONE_RUNNER_NAME' ], image='alpine') ] + steps,
};

[
  pipeline('build', [
    step(
      'deps',
      [
        'make deps',
        './scripts/enforce-clean',
      ],
      dependencies=[ 'runner identification' ],
    ),

    step(
      'lint',
      [ 'make lint' ],
      dependencies=[ 'deps' ],
    ),

    step(
      'test',
      [ 'make test' ],
      dependencies=[ 'deps' ],
    ),

    step(
      'build',
      [ 'make build' ],
      dependencies=[ 'lint', 'test' ],
    ),
  ]) + {
    trigger+: {
      ref+: [
        'refs/heads/main',
        'refs/pull/**',
      ],
    },
  },

  pipeline('release', [
  ]) + {
    trigger+: {
      ref+: [
        'refs/tags/v*.*.*',
      ],
    },
  },

  vault_secret('gcr_sa', 'infra/data/ci/gcr-admin', 'service-account'),
  vault_secret('docker_config_json', 'secret/data/common/gcr', '.dockerconfigjson'),
  vault_secret('argo_token', 'infra/data/ci/argo-workflows/trigger-service-account', 'token'),
]
