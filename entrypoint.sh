#!/bin/sh
chown -R appuser:root /data/uploads /data
exec su-exec appuser "$@"
