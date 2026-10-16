FROM nginx:1.27-alpine
ADD --chmod=644 --checksum=sha256:040db4c0db72e7f7599cd0e06b0f58f95e0082a395cdde410ca26224a79a0626 https://cdn.jsdelivr.net/npm/livekit-client@2.22.4/dist/livekit-client.umd.min.js /usr/share/nginx/html/livekit-client.js
COPY index.html /usr/share/nginx/html/
RUN sed -i 's/listen\s*80;/listen 8080;/' /etc/nginx/conf.d/default.conf
EXPOSE 8080
CMD ["nginx", "-g", "daemon off;"]
