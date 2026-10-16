FROM rtcbench-mediasoup:latest AS src
FROM nginx:1.27-alpine
RUN apk add --no-cache openssl && \
    openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
    -keyout /etc/nginx/tls.key -out /etc/nginx/tls.crt -days 3650 -nodes \
    -subj "/CN=mediasoup-web"
COPY --from=src /app/public /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 8080
CMD ["nginx", "-g", "daemon off;"]
