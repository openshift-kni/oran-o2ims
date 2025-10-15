# troubleshooting

## Using the development debug mode to attach the DLV debugger

The following instructions provide a mechanism to build an image that is based on a more full-featured distro so that
debug tools are available in the image. It also tailors the deployment configuration so that certain features are
disabled which would otherwise cause debugging with a debugger to be more difficult (or impossible).

1. Build and deploy the debug image

   ```console
   make IMAGE_TAG_BASE=quay.io/${USER}/oran-o2ims VERSION=latest DEBUG=yes build docker-build docker-push install deploy
   ```

2. Forward a port to the Pod to be debugged so the debugger can attach to it. This command will remain active until
   it is terminated with ctrl+c therefore you will need to execute it in a dedicated window (or move it to the
   background).

   ```console
   oc port-forward -n oran-o2ims pods/oran-o2ims-controller-manager-85b4bbcf58-4fc9s 40000:40000
   ```

3. Execute a shell into the Pod to be debugged.

   ```console
   oc rsh -n oran-o2ims pods/oran-o2ims-controller-manager-85b4bbcf58-4fc9s
   ```

4. Attach the DLV debugger to the process. This is usually PID 1, but this may vary based on the deployment. Use the
   same port number that was specified earlier in the `port-forward` command.

   ```console
   dlv attach --continue --accept-multiclient --api-version 2 --headless --listen :40000 --log 1
   ```

5. Use your IDE's debug capabilities to attach to `localhost:40000` to start your debug session. This will vary based
   on which IDE is being used.
