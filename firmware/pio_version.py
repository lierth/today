# Injects the firmware version as TODAY_FW_VERSION. PlatformIO lets an extra
# script add a CPPDEFINE; the release script sets TODAY_FW_VERSION=<tag without
# v>, and a local build falls back to a development string that no release will
# ever be newer than (see firmware/src/version.h).
import os

Import("env")

version = os.environ.get("TODAY_FW_VERSION", "0.0.0-dev").strip() or "0.0.0-dev"
env.Append(CPPDEFINES=[("TODAY_FW_VERSION", '\\"%s\\"' % version)])
print("firmware version: %s" % version)
