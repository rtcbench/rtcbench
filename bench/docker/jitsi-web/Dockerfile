FROM nginx:alpine
COPY index.html /usr/share/nginx/html/index.html
RUN echo 'server { listen 8080; location / { root /usr/share/nginx/html; } }' > /etc/nginx/conf.d/default.conf
EXPOSE 8080
