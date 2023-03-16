NAMESPACE=csi-smb-controller
TEST_NAMESPACE=csi-smb-test
oc apply -f - <<EOF
---
apiVersion: v1
kind: Namespace
metadata:
  labels:
    security.openshift.io/scc.podSecurityLabelSync: "false"
  name: ${TEST_NAMESPACE}
EOF

# Persistent volume from which PVC's will be claimed
# Pods are having an issue using the service DNS,
# smb-server.csi-smb-controller.svc.cluster.local,
# so just using an IP for now. 
oc apply -f - << EOF
---
apiVersion: v1
kind: PersistentVolume
metadata:
  name: pv-smb
  namespace: $TEST_NAMESPACE
spec:
  storageClassName: smb
  capacity:
    storage: 20Gi
  accessModes:
    - ReadWriteMany
  persistentVolumeReclaimPolicy: Retain
  mountOptions:
    - dir_mode=0777
    - file_mode=0777
    - uid=1001
    - gid=1001
    - noperm
    - mfsymlinks
    - cache=strict
    - noserverino  # required to prevent data corruption
  csi:
    driver: smb.csi.k8s.io
    readOnly: false
    volumeHandle: smb-vol-1  # make sure it's a unique id in the cluster
    volumeAttributes:
      source: //172.30.209.153/share
    nodeStageSecretRef:
      name: smbcreds
      namespace: $NAMESPACE
EOF

# PVC which can be used by a pod to access the SMB server
oc apply -f - <<EOF
---
kind: PersistentVolumeClaim
apiVersion: v1
metadata:
  name: pvc-smb
  namespace: $TEST_NAMESPACE
spec:
  accessModes:
    - ReadWriteMany
  resources:
    requests:
      storage: 10Gi
  volumeName: pv-smb
  storageClassName: smb
EOF

oc apply -n $TEST_NAMESPACE -f - << 'EOF'
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: smb-writer
  labels:
    app: smb-writer
spec:
  replicas: 1
  template:
    metadata:
      name: smb-writer
      labels:
        app: smb-writer
    spec:
      tolerations:
      - key: "os"
        value: "Windows"
        Effect: "NoSchedule"
      nodeSelector:
        "kubernetes.io/os": windows
      containers:
        - name: writer
          image: mcr.microsoft.com/powershell:lts-nanoserver-ltsc2022
          command:
            - "pwsh.exe"
            - "-Command"
            - "while (1) { Add-Content -Encoding Ascii C:\\mnt\\smb\\data.txt $(Get-Date -Format u); sleep 10 }"
          volumeMounts:
            - name: smb
              mountPath: "/mnt/smb"
      volumes:
        - name: smb
          persistentVolumeClaim:
            claimName: pvc-smb
  selector:
    matchLabels:
      app: smb-writer
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: smb-reader
  labels:
    app: smb-reader
spec:
  replicas: 1
  template:
    metadata:
      name: smb-reader
      labels:
        app: smb-reader
    spec:
      tolerations:
      - key: "os"
        value: "Windows"
        Effect: "NoSchedule"
      nodeSelector:
        "kubernetes.io/os": windows
      containers:
        - name: reader
          image: mcr.microsoft.com/powershell:lts-nanoserver-ltsc2022
          command:
            - "pwsh.exe"
            - "-Command"
            - "while (1) { Write-Host 'Reading from smb mount:'; Get-Content -Path C:\\mnt\\smb\\data.txt; sleep 20 }"
          volumeMounts:
            - name: smb
              mountPath: "/mnt/smb"
      volumes:
        - name: smb
          persistentVolumeClaim:
            claimName: pvc-smb
  selector:
    matchLabels:
      app: smb-reader
EOF
