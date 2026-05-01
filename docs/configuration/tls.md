# TLS Configuration

## Direct TLS on the Server

Set `server.tls.enabled: true` in your Helm values and provide a TLS Secret named `kerrareg-tls`:

```yaml
server:
  tls:
    enabled: true
    certPath: /etc/tls/tls.crt
    keyPath: /etc/tls/tls.key
```
!!! note
    When TLS is enabled, the server listens on port `443` instead of `8080`. Ensure your Service `targetPort` and any probes are updated accordingly.

!!! note
    When `anonymousAuth` is enabled, the server uses its own ServiceAccount to query the Kubernetes API for Module and Version resources. No client credentials are required. The server's ClusterRole only permits reading `modules` and `versions`, so anonymous users cannot create or modify resources.

## TLS via Istio Ingress Gateway

For TLS termination at the Istio ingress gateway, enable the Istio VirtualService and create a Gateway resource. The chart's VirtualService references the gateway `istio-ingress/istio-ingress-gateway` by default. See [chart/kerrareg/istio/gateway.yaml](https://github.com/tonedefdev/kerrareg/blob/main/chart/kerrareg/istio/gateway.yaml) for an example, and store your TLS certificate as a Secret in the `istio-ingress` namespace:

```yaml
server:
  ingress:
    istio:
      enabled: true
      hosts:
        - kerrareg.defdev.io
```
