#!/usr/bin/env bash
set -euo pipefail

echo "bash=$(command -v bash || true)"
echo "python3=$(command -v python3 || true)"
echo "java=$(command -v java || true)"
echo "grep=$(command -v grep || true)"
echo "sed=$(command -v sed || true)"
echo "awk=$(command -v awk || true)"

project_resources="${KOGITO_HOME}/serverless-workflow-project/src/main/resources/"

OUTPUT_FILE=/tmp/metadata.info
> "${OUTPUT_FILE}"

find ${project_resources} \
  \( -name "*.sw.json" -o -name "*.sw.yaml" -o -name "*.sw.yml" \) \
  | while read -r wf; do

    if [[ "$wf" == *.json ]]; then
        id=$(jq -r '.id' "$wf")
        version=$(jq -r '.version' "$wf")
    else
        id=$(yq -r '.id' "$wf")
        version=$(yq -r '.version' "$wf")
    fi

    if [[ -n "$id" && "$id" != "null" && \
          -n "$version" && "$version" != "null" ]]; then
        echo "${id}:${version}" >> "${OUTPUT_FILE}"
    fi
done

sort -u "${OUTPUT_FILE}" -o "${OUTPUT_FILE}"