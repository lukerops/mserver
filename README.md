## How to run

```sh
podman build --tag mserver:develop .
podman run --rm -it --name mserver -p 8096:8096 -v ./build/movies:/media/movies:ro mserver:develop
```
