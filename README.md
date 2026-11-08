## How to run

```
export MSERVER_PRIVATE_TOKEN=$(openssl rand -hex 32)
./mserver &
curl -X POST -H "Authorization: Bearer $MSERVER_PRIVATE_TOKEN" \
  -d '{"username":"admin","email":"a@b.c","password":"secret123","profile":{"handle":"lucas","display_name":"Lucas"}}' \
  http://127.0.0.1:8096/v1/setup
```
