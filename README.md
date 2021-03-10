# HPE CSI Driver for Kubernetes

A Container Storage Interface ([CSI](https://github.com/container-storage-interface/spec)) Driver for Kubernetes. The HPE CSI Driver for Kubernetes allows you to use a [Container Storage Provider](https://github.com/hpe-storage/container-storage-provider) (CSP) to perform data management operations on storage resources.

## Releases

The CSI driver is released with a set of container images published on Quay. Those are in turn referenced from deployment manifests, Helm charts and Operators (see next section). Relases are rolled up on the [HPE Storage Container Orchestrator Documentation](https://scod.hpedev.io/csi_driver/index.html#compatibility_and_support) (SCOD) portal.

- Release notes are hosted in [release-notes](release-notes).

## Deploying and using the CSI driver on Kubernetes

All documentation for installing and using the CSI driver with a HPE storage backend (CSP) is available on [SCOD](https://scod.hpedev.io/csi_driver/deployment.html).

Release vehicles include:

- [HPE CSI Driver for Kubernetes Helm chart](https://artifacthub.io/packages/helm/hpe-storage/hpe-csi-driver)
- [HPE CSI Operator for Kubernetes](https://operatorhub.io/operator/hpe-csi-operator).

Source deployment manifests are available in [hpe-storage/co-deployments](https://github.com/hpe-storage/co-deployments).

## Building

Instructions on how to build the HPE CSI Driver can be found in [BUILDING.md](BUILDING.md).

## Testing

Example Kubernetes object definitions used to build test cases for the CSI driver are available in [examples](examples).

## Support

The HPE CSI Driver for Kubernetes 1.0.0 and onwards is fully supported by HPE and is Generally Available. Certain CSI features may be subject to alpha and beta status. Refer to the [official table](https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/) of feature gates in the Kubernetes documentation to find availability of beta features.

Formal support statements for each HPE supported CSP is [available on SCOD](https://scod.hpedev.io/legal/support). Use this facility for formal support of your HPE storage products, including the CSI driver.

## Community

Please file any issues, questions or feature requests you may have [here](https://github.com/hpe-storage/csi-driver/issues) (do not use this facility for support inquiries of your HPE storage product, see [SCOD](https://scod.hpedev.io/legal/support) for support). You may also join our Slack community to chat with HPE folks close to this project. We hang out in `#NimbleStorage`, `#3par-primera`, `#hpe-cloud-volumes` and `#Kubernetes`. Sign up at [slack.hpedev.io](https://slack.hpedev.io/) and login at [hpedev.slack.com](https://hpedev.slack.com/)

## Contributing

We value all feedback and contributions. If you find any issues or want to contribute, please feel free to open an issue or file a PR. More details in [CONTRIBUTING.md](CONTRIBUTING.md)

## License

This is open source software licensed using the Apache License 2.0. Please see [LICENSE](LICENSE) for details.
