cd #!/bin/bash

if [ $MICRO_ENV == "dev" ]; then
  sed -i '' 's/\*.m3o.app/\*.mcbeam.dev/g' 'ls ./service'
  sed -i '' 's/m3o.com/mcbeam.dev/g' ingress.yaml
fi

if [ $MICRO_ENV == "dev" ]; then
  sed -i '' 's/ccr.ccs.tencentyun.com\/wolfplus\/mcbeam/ccr.ccs.tencentyun.com\/wolfplus\/mcbeam:mcbeam-v3-202011071039/g'
  sed -i '' '/containers:/i\\      imagePullSecrets:\n      - name: qcloudregistrykey'

kubectl apply -f service

win:
  sed -i '' 's/mcbeam-v3-202012111405/mcbeam-v3-202012151058/g' `ls ./service/*`

kubectl run cockroachdb --image=ubuntu -it --rm --restart=Never --overrides='
{
  "spec": {
    "template": {
      "spec": {
        "containers": [
          {
            "image": "ubuntu",
            "volumeMounts": [{
              "readOnly": true,
              "mountPath": "/certs/store",
              "name": "cockroachdb-client-certs"
            }]
          }
        ],
        "volumes": [{
          "name":"cockroachdb-client-certs",
          "secret": {
            "secretName": "cockroachdb-client-certs",
            "defaultMode": "0600"
          }
        }]
      }
    }
  }
}
' -- bash
