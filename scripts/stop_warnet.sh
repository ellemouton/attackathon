#!/bin/bash
#
if [ ! -d "warnet" ]; then
    echo "Error: warnet directory not found."
	exit 1
fi

cd warnet

echo "Removing carla remote"
echo "TODO: remove me when attackathon/18 has been addressed!"
git remote remove carla
git checkout main

docker_info=$(docker info)

# Setup depends on docker or docker desktop.
if grep -q "Operating System:.*Desktop" <<< "$docker_info"; then
    echo "Stopping warnet for docker desktop."
    just stopd
else
    echo "Stopping warnet for minikube."
    just stop
fi
