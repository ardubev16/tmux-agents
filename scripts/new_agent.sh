#!/usr/bin/env bash

. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/_herdr.sh"

agent_name=$(new_agent "$1" "$2")
attach_agent "$agent_name"
