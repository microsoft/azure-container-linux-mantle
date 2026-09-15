#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# Licensed under the MIT License.

# Test the Azure Linux builder's crypto setting and fail-fast build sequence.
# Run with: bash tests/test_dockerfile_azl.sh
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
dockerfile="${repo_root}/Dockerfile.azl"
scratch=$(mktemp -d)
trap 'rm -rf "${scratch}"' EXIT
failures=0

if grep -Eq '^ENV MS_GO_NOSYSTEMCRYPTO=1$' "${dockerfile}" &&
    ! grep -Eq '^ENV GOEXPERIMENT=.*nosystemcrypto' "${dockerfile}"; then
    echo 'PASS: portable crypto uses the supported Microsoft Go setting'
else
    echo 'FAIL: portable crypto still uses a removed Go experiment'
    failures=$((failures + 1))
fi

# Exercise the Dockerfile's actual RUN shell command with a fake ./build.
# Redirect only its working directory and use Docker's default /bin/sh shell.
build_command=$(sed -n '/^RUN .*\.\/build/p' "${dockerfile}")
build_command=${build_command#RUN }
if [[ "${build_command}" != 'cd /usr/src/mantle && '* || "${build_command}" == *$'\n'* ]]; then
    echo 'FAIL: expected one Docker RUN command for both architecture builds'
    exit 1
fi
build_command=${build_command/\/usr\/src\/mantle/\"\$TEST_BUILD_DIR\"}

for scenario in success fail-amd64 fail-arm64; do
    case_dir="${scratch}/${scenario}"
    mkdir -p "${case_dir}"
    cat > "${case_dir}/build" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
arch=${GOARCH:-amd64}
printf '%s:%s\n' "${arch}" "${CGO_ENABLED:-unset}" >> calls
# Partial output must not hide a failing build.
mkdir -p bin
printf '%s\n' "${arch}" > bin/architecture
[[ "${TEST_SCENARIO}" != "fail-${arch}" ]] || exit 42
EOF
    chmod +x "${case_dir}/build"
    status=0
    TEST_BUILD_DIR="${case_dir}" TEST_SCENARIO="${scenario}" \
        GOARCH=amd64 CGO_ENABLED=1 /bin/sh -c "${build_command}" > "${case_dir}/output" 2>&1 || status=$?

    case "${scenario}" in
        success)
            if [[ ${status} -eq 0 && -f "${case_dir}/bin-amd64/architecture" &&
                -f "${case_dir}/bin-arm64/architecture" &&
                "$(cat "${case_dir}/bin-amd64/architecture")" == amd64 &&
                "$(cat "${case_dir}/bin-arm64/architecture")" == arm64 &&
                "$(cat "${case_dir}/calls")" == $'amd64:1\narm64:0' ]]; then
                echo 'PASS: both architecture outputs use the expected CGO settings'
            else
                cat "${case_dir}/output"
                echo 'FAIL: successful builds did not produce both architectures'
                failures=$((failures + 1))
            fi
            ;;
        fail-amd64)
            if [[ ${status} -eq 42 && "$(cat "${case_dir}/calls")" == amd64:1 &&
                ! -d "${case_dir}/bin-amd64" && ! -d "${case_dir}/bin-arm64" ]]; then
                echo 'PASS: amd64 build failure stops before renaming or cross-building'
            else
                echo 'FAIL: amd64 build failure was masked'
                failures=$((failures + 1))
            fi
            ;;
        fail-arm64)
            if [[ ${status} -eq 42 && -d "${case_dir}/bin-amd64" &&
                "$(cat "${case_dir}/calls")" == $'amd64:1\narm64:0' &&
                ! -d "${case_dir}/bin-arm64" ]]; then
                echo 'PASS: arm64 build failure stops before renaming partial output'
            else
                echo 'FAIL: arm64 build failure was masked'
                failures=$((failures + 1))
            fi
            ;;
    esac
done

[[ ${failures} -eq 0 ]]
