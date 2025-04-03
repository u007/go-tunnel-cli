
# how it works?

Goal: The program's primary function is to establish a secure SSH tunnel and then execute a specified command, making the tunnelled connection available to that command.

It load variables from this file (e.g., SSH_HOST, SSH_PORT, SSH_USERNAME, SSH_KEYFILE, SSH_PASSWORD, SSH_LOCALPORT, SSH_REMOTEHOST, SSH_REMOTEPORT)
to connect to remote host.
and bind a localport to forward to remote host and port and set it as environment variable:

```
SSH_LOCALPORT
```


To run 

```
tunnel test.env bun sequelize.test.ts
```

## Debug

```
DEBUG=1 ./tunnel test.env bun sequelize.test.ts
```

or add DEBUG=1 to your env file

# build

```
make build
```

# license

MIT License
