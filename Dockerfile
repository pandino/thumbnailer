FROM alpine:3

LABEL maintainer="Movie Thumbnailer"
LABEL description="A container for generating movie thumbnail mosaics using ffmpeg tile filters"

# Install required dependencies
RUN apk add --no-cache \
    bash \
    ffmpeg \
    sqlite \
    coreutils \
    bc \
    findutils

# Set up directories
RUN mkdir -p /app /movies /thumbnails /data \
    && chown -R 1000:1000 /app /movies /thumbnails /data

# Copy the script
COPY movie-thumbnailer.sh /app/
RUN chmod +x /app/movie-thumbnailer.sh \
    && chown 1000:1000 /app/movie-thumbnailer.sh

# Set working directory
WORKDIR /app

# Switch to non-root user
USER 1000

# Set default environment variables
ENV INPUT_DIR=/movies
ENV OUTPUT_DIR=/thumbnails
ENV DB_FILE=/data/thumbnailer.db
ENV MAX_WORKERS=4

# Volume configuration
VOLUME ["/movies", "/thumbnails", "/data"]

# Default command
ENTRYPOINT ["/app/movie-thumbnailer.sh"]
CMD ["--help"]
