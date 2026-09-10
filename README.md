# NGXStorage logutils

Shared Zap logging for the NGXStorage FC, iSCSI and NFS CSI drivers.

Node deployments set `NGXSTORAGE_CSI_LOG_DIR` to the protocol-specific
`/host/var/log/ngxstorage/<protocol>` directory. `FilePrefix` scopes rotation
and retention to one driver instance. An empty directory keeps logging on
stdout and does not create a temporary file.

```go
instance, err := logutils.NewFromEnvironment("info", nodeID, "nfs", driverNamePrefix)
if err != nil { return err }
defer instance.Close()
csiDriver, err := driver.NewDriver(driver.InputParameters{Logger: instance.Logger})
```

With an empty `driverNamePrefix`, node files start with `nfs-`. A prefix such
as `201` produces `nfs-201-...log`, so rotation and retention from another
installation (for example `205`) cannot select those files. Controllers do not
set the directory environment variable and therefore remain stdout-only.

Each process owns its logger `Instance`; `Close` stops and joins maintenance
before syncing and closing the file. There is no package-global logger.
