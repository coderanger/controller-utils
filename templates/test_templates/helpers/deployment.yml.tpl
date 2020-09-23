{{ define "deployment" }}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-{{ block "componentName" . }}{{ end }}
spec:
  replicas: {{ block "replicas" . }}1{{ end }}
  selector:
    matchLabels:
      app: test
  template:
    metadata:
      labels:
        app: test
    spec:
      containers:
      - name: default
        image: test
{{ end }}
