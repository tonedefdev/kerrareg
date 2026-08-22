version_settings(constraint='>=0.33.20')
allow_k8s_contexts('kind-opendepot')
update_settings(max_parallel_updates=6)

load('ext://restart_process', 'docker_build_with_restart')

go_image_ignores = [
    '**/*_test.go',
    '**/bin',
    '**/bin/**',
    '**/config',
    '**/config/**',
    '**/hack',
    '**/hack/**',
    '**/test',
    '**/test/**',
    '**/cover.out',
    '**/Dockerfile',
    '**/Makefile',
    '**/PROJECT',
    '**/README.md',
    '**/sample.yaml',
]

docker_build_with_restart(
    'ghcr.io/tonedefdev/opendepot/server',
    '.',
    dockerfile='tilt/Dockerfile.go',
    target='server-runtime',
    only=['tilt/Dockerfile.go', 'services/server', 'api/v1alpha1', 'pkg/storage', 'pkg/utils'],
    ignore=go_image_ignores,
    entrypoint=['/workspace/bin/server'],
    live_update=[
        fall_back_on([
            'api/v1alpha1/go.mod',
            'api/v1alpha1/go.sum',
            'pkg/storage/go.mod',
            'pkg/storage/go.sum',
            'pkg/utils/go.mod',
            'pkg/utils/go.sum',
            'services/server/go.mod',
            'services/server/go.sum',
            'tilt/Dockerfile.go',
        ]),
        sync('services/server', '/workspace/services/server'),
        sync('api/v1alpha1', '/workspace/api/v1alpha1'),
        sync('pkg/storage', '/workspace/pkg/storage'),
        sync('pkg/utils', '/workspace/pkg/utils'),
        run('cd /workspace/services/server && CGO_ENABLED=0 go build -o /workspace/bin/server .', trigger=[
            'services/server',
            'api/v1alpha1',
            'pkg/storage',
            'pkg/utils',
        ]),
    ],
)

docker_build_with_restart(
    'ghcr.io/tonedefdev/opendepot/depot-controller',
    '.',
    dockerfile='tilt/Dockerfile.go',
    target='depot-runtime',
    only=['tilt/Dockerfile.go', 'services/depot', 'api/v1alpha1', 'pkg/github', 'pkg/registry'],
    ignore=go_image_ignores,
    entrypoint=['/workspace/bin/depot-controller'],
    live_update=[
        fall_back_on([
            'api/v1alpha1/go.mod',
            'api/v1alpha1/go.sum',
            'pkg/github/go.mod',
            'pkg/github/go.sum',
            'pkg/registry/go.mod',
            'services/depot/go.mod',
            'services/depot/go.sum',
            'tilt/Dockerfile.go',
        ]),
        sync('services/depot', '/workspace/services/depot'),
        sync('api/v1alpha1', '/workspace/api/v1alpha1'),
        sync('pkg/github', '/workspace/pkg/github'),
        sync('pkg/registry', '/workspace/pkg/registry'),
        run('cd /workspace/services/depot && CGO_ENABLED=0 go build -o /workspace/bin/depot-controller ./cmd', trigger=[
            'services/depot',
            'api/v1alpha1',
            'pkg/github',
            'pkg/registry',
        ]),
    ],
)

docker_build_with_restart(
    'ghcr.io/tonedefdev/opendepot/module-controller',
    '.',
    dockerfile='tilt/Dockerfile.go',
    target='module-runtime',
    only=['tilt/Dockerfile.go', 'services/module', 'api/v1alpha1', 'pkg/utils'],
    ignore=go_image_ignores,
    entrypoint=['/workspace/bin/module-controller'],
    live_update=[
        fall_back_on([
            'api/v1alpha1/go.mod',
            'api/v1alpha1/go.sum',
            'pkg/utils/go.mod',
            'pkg/utils/go.sum',
            'services/module/go.mod',
            'services/module/go.sum',
            'tilt/Dockerfile.go',
        ]),
        sync('services/module', '/workspace/services/module'),
        sync('api/v1alpha1', '/workspace/api/v1alpha1'),
        sync('pkg/utils', '/workspace/pkg/utils'),
        run('cd /workspace/services/module && CGO_ENABLED=0 go build -o /workspace/bin/module-controller ./cmd', trigger=[
            'services/module',
            'api/v1alpha1',
            'pkg/utils',
        ]),
    ],
)

docker_build_with_restart(
    'ghcr.io/tonedefdev/opendepot/provider-controller',
    '.',
    dockerfile='tilt/Dockerfile.go',
    target='provider-runtime',
    only=['tilt/Dockerfile.go', 'services/provider', 'api/v1alpha1', 'pkg/utils'],
    ignore=go_image_ignores,
    entrypoint=['/workspace/bin/provider-controller'],
    live_update=[
        fall_back_on([
            'api/v1alpha1/go.mod',
            'api/v1alpha1/go.sum',
            'pkg/utils/go.mod',
            'pkg/utils/go.sum',
            'services/provider/go.mod',
            'services/provider/go.sum',
            'tilt/Dockerfile.go',
        ]),
        sync('services/provider', '/workspace/services/provider'),
        sync('api/v1alpha1', '/workspace/api/v1alpha1'),
        sync('pkg/utils', '/workspace/pkg/utils'),
        run('cd /workspace/services/provider && CGO_ENABLED=0 go build -o /workspace/bin/provider-controller ./cmd', trigger=[
            'services/provider',
            'api/v1alpha1',
            'pkg/utils',
        ]),
    ],
)

docker_build_with_restart(
    'ghcr.io/tonedefdev/opendepot/version-controller',
    '.',
    dockerfile='tilt/Dockerfile.go',
    target='version-dev',
    only=['tilt/Dockerfile.go', 'services/version', 'api/v1alpha1', 'pkg/github', 'pkg/registry', 'pkg/storage', 'pkg/utils'],
    ignore=go_image_ignores,
    entrypoint=['/workspace/bin/version-controller'],
    live_update=[
        fall_back_on([
            'api/v1alpha1/go.mod',
            'api/v1alpha1/go.sum',
            'pkg/github/go.mod',
            'pkg/github/go.sum',
            'pkg/registry/go.mod',
            'pkg/storage/go.mod',
            'pkg/storage/go.sum',
            'pkg/utils/go.mod',
            'pkg/utils/go.sum',
            'services/version/go.mod',
            'services/version/go.sum',
            'tilt/Dockerfile.go',
        ]),
        sync('services/version', '/workspace/services/version'),
        sync('api/v1alpha1', '/workspace/api/v1alpha1'),
        sync('pkg/github', '/workspace/pkg/github'),
        sync('pkg/registry', '/workspace/pkg/registry'),
        sync('pkg/storage', '/workspace/pkg/storage'),
        sync('pkg/utils', '/workspace/pkg/utils'),
        run('cd /workspace/services/version && CGO_ENABLED=0 go build -o /workspace/bin/version-controller ./cmd', trigger=[
            'services/version',
            'api/v1alpha1',
            'pkg/github',
            'pkg/registry',
            'pkg/storage',
            'pkg/utils',
        ]),
    ],
)

docker_build(
    'ghcr.io/tonedefdev/opendepot/ui',
    'services/ui',
    dockerfile='services/ui/Dockerfile.dev',
    live_update=[
        sync('services/ui/src', '/app/src'),
        sync('services/ui/public', '/app/public'),
    ],
    ignore=['.next', 'node_modules', 'coverage'],
)

k8s_yaml(helm(
    'chart/opendepot',
    name='opendepot',
    namespace='opendepot-system',
    values=['tilt/values.yaml', 'tilt/.generated/values.yaml'],
))

k8s_resource('valkey', labels=['infrastructure'])
k8s_resource('opendepot-dex', resource_deps=['valkey'], labels=['infrastructure'])
k8s_resource('server', resource_deps=['opendepot-dex'], labels=['backend'])
k8s_resource('module-controller', resource_deps=['server'], labels=['backend'])
k8s_resource('depot-controller', resource_deps=['server'], labels=['backend'])
k8s_resource('provider-controller', resource_deps=['server'], labels=['backend'])
k8s_resource('version-controller', resource_deps=['server'], labels=['backend'])
k8s_resource(
    'ui',
    resource_deps=['server'],
    port_forwards=[port_forward(8080, 8080, name='OpenDepot UI')],
    links=[link('http://opendepot.localtest.me:8080', 'OpenDepot UI')],
    labels=['frontend'],
)

local_resource(
    'seed-sample-resources',
    cmd='tilt/scripts/seed-resources.sh',
    resource_deps=['module-controller', 'provider-controller'],
    auto_init=False,
    labels=['controls'],
)
local_resource(
    'clear-sample-resources',
    cmd='tilt/scripts/clear-resources.sh',
    auto_init=False,
    labels=['controls'],
)
local_resource(
    'seed-trivy-db',
    cmd='tilt/scripts/refresh-trivy.sh',
    resource_deps=['version-controller'],
    labels=['infrastructure'],
)
local_resource(
    'refresh-trivy-db',
    cmd='tilt/scripts/refresh-trivy.sh',
    resource_deps=['version-controller'],
    auto_init=False,
    labels=['controls'],
)
local_resource(
    'reset-cluster',
    cmd='tilt/scripts/reset-cluster.sh',
    auto_init=False,
    labels=['controls'],
)