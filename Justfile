image := "the-lab-zone-slack-agents"
tag := "latest"

default:
    @just --list

build:
    docker build -t {{image}}:{{tag}} .

build-tag tag_name:
    docker build -t {{image}}:{{tag_name}} .
