# HPE CSI Driver for Kubernetes
A Container Storage Interface ([CSI](https://github.com/container-storage-interface/spec)) Driver for Kubernetes. The HPE CSI Driver for Kubernetes allows you to use a [Container Storage Provider](https://github.com/hpe-storage/container-storage-provider) to perform data management operations on storage resources.

## Kubernetes Compatibility

<table>
  <thead>
    <tr>
      <th></th>
      <th colspan=3>Kubernetes Version</th>
      <th rowspan=2> Release Notes</th>
    </tr>
    <tr>
      <th>HPE CSI Driver</th>
      <th><= 1.12</th>
      <th>1.13+</th>
      <th>1.14+</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td>v1.0.0</td>
      <td>no</td>
      <td>yes</td>
      <td>yes</td>
      <td><a href="release-notes/v1.0.0.md">v1.0.0</a></td>
    </tr>
  </tbody>
  <tbody>
    <tr>
      <td>v1.1.0</td>
      <td>no</td>
      <td>yes</td>
      <td>yes</td>
      <td><a href="release-notes/v1.1.0.md">v1.1.0</a></td>
    </tr>
  </tbody>
</table>

**Note:** Support matrix for each release can be found on SCOD: [compatability and support](https://scod.hpedev.io/csi_driver/index.html#compatibility_and_support)

## Deploying and using the CSI driver on Kubernetes
All documentation for installing and using the HPE CSI Driver for Kubernetes is available on the [HPE Storage Container Orchestrator Documentation](https://scod.hpedev.io/csi_driver) (SCOD) portal.

## Building the HPE CSI Driver

Instructions on how to build the HPE CSI Driver can be found in [BUILDING.md](BUILDING.md)

## CSI examples
Example Kubernetes object definitions used to build test cases for the CSI driver is a available in [examples](examples).

## Support

The HPE CSI Driver 1.0 is fully supported and is Generally Available. Other features like volume expansion, raw block volumes, CSI snapshots are considered beta (Do not use these features for production and do not contact HPE for support). Refer to the [official table](https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/) of feature gates in the Kubernetes docs to find availability of beta features. Please file any issues, questions or feature requests [here](https://github.com/hpe-storage/csi-driver/issues). You may also join our Slack community to chat with HPE folks close to this project. We hang out in `#NimbleStorage` and `#Kubernetes` at [slack.hpedev.io](https://slack.hpedev.io/).

## Contributing

We value all feedback and contributions. If you find any issues or want to contribute, please feel free to open an issue or file a PR. More details in [CONTRIBUTING.md](CONTRIBUTING.md)

## License

This is open source software licensed using the Apache License 2.0. Please see [LICENSE](LICENSE) for details.
