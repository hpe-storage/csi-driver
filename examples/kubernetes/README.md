There are couple of options to deploy nimble csi plugin, let use one of simple approach and test your plugin.
Here are the high level steps:

Pre-reqs
    k8s 1.13 and above
    nimble array v5.1.3 and above


### Quickstart guide to deploy csi plugin 

step 1:
    clone git@github.com:hpe-storage/co-deployments.git
    ```
        git clone git@github.com:hpe-storage/co-deployments.git
        cd co-deployments
    ```
step 2:
        run helm command
        edit ./helm/charts/hpe-csi-driver/values.yaml
        ```
        helm install hpe-csi ./helm/charts/hpe-csi-driver/ --namespace kube-system
        ```
step 3:
    check the plugin status by running these commands, make sure there are no failures
    ```
        kubectl get pods -n kube-system
        kubectl get sc
        kubectl logs -f deployment.apps/hpe-csi-controller hpe-csi-driver -n kube-system
        kubectl logs  -n kube-system hpe-csi-node-87ncp -c csi-node-driver-registrar
        ssh to one of the node and run below command
        systemctl status hpe-storage-node.service
    ```

### How to run sample examples to verify:

step 1:
    clone git@github.com:hpe-storage/csi-driver.git
    cd  examples/kubernetes
    edit hpe-nimble-storage/secret.yaml
    run below command
    ```
        kubectl apply -f hpe-nimble-storage/secret.yaml
        kubectl apply -f hpe-nimble-storage/storage-class.yaml
    ```

step 2:
    deploy pvc and pods
    ```
    kubectl apply -f pvc.yaml
    kubectl apply -f pod.yaml
    ```

step 3:
    verify the pod is created and you see pvc volume in the array
    ```
        kubectl get pvc
        kubectl get pod
    ```
