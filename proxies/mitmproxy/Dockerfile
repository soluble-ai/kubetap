FROM mitmproxy/mitmproxy:latest

# mitmproxy is built on alpine:3.11, so in case the image is old
# (and lazily avoiding having to build it ourselves for now), update
RUN apk upgrade -U --no-cache
# && pip3 install -U pip  # latest version of pip is buggy - 08/17/2020

# HACK: the security context of the injected pod could be run as any user, therefore
# all users must be able to write to the directory.
RUN chmod -R 777 /home/mitmproxy/.mitmproxy/

# Hijack the mitmproxy entrypoint (docker-entrypoint.sh) so that
# configuration can be built from within the container using the
# kubetap binary.

COPY kubetap-entrypoint.sh /usr/local/bin/
ENTRYPOINT ["kubetap-entrypoint.sh"]
