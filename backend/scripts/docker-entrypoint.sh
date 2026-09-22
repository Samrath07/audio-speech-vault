#!/bin/sh
set -eu

audio-speech-vault-migrate up
exec audio-speech-vault-server
