FROM jitsi/web:stable-9646

# Append VP9 overrides to the base config.js template.
# The entrypoint runs: tpl /defaults/config.js > /config/config.js
# config.additions.js uses plain JS assignment so it runs after var config={...}.
COPY config.additions.js /tmp/config.additions.js
RUN cat /tmp/config.additions.js >> /defaults/config.js
