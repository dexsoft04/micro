cd #!/bin/bash

if [ $MICRO_ENV == "dev" ]; then
  sed -i '' 's/\*.m3o.app/\*.mcbeam.dev/g' 'ls ./service'
  sed -i '' 's/m3o.com/mcbeam.dev/g' ingress.yaml
fi

if [ $MICRO_ENV == "dev" ]; then
  sed -i '' 's/mcbeam:mcbeam-v3-20210917-1eda672f8a83f2f9103e5985e51a7893c0a8f1d4/mcbeam:mcbeam-v3-20210928-483fb4aece9e21601ed901a49ffa79d73aae4b17/g' `ls ./service/*`
  sed -i '' 's/- name: qcloudregistrykey//g'  `ls ./service/*`
kubectl apply -f service

win:
  sed -i '' 's/mcbeam.tencentcloudcr.com\/wolfplus\/mcbeam:mcbeam-0311/mcbeam-hub.tencentcloudcr.com\/wolfplus\/mcbeam-dev:mcbeam-dev-20210809-668f94da117f906a3259f671f815dce6e169602b/g' `ls ./service/*`
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
