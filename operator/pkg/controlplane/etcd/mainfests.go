package etcd

const (
	// KarmadaEtcdStatefulSet is karmada etcd StatefulSet manifest
	KarmadaEtcdStatefulSet = `
apiVersion: apps/v1
kind: StatefulSet
metadata:
  labels:
    karmada-app: etcd
    app.kubernetes.io/managed-by: karmada-operator
  namespace: {{ .Namespace }}
  name: {{ .StatefulSetName }}
spec:
  replicas: {{ .Replicas }}
  serviceName: {{ .StatefulSetName }}
  podManagementPolicy: Parallel
  selector:
    matchLabels:
      karmada-app: etcd
      karmada-etcd: {{ .StatefulSetName }}
  template:
    metadata:
      labels:
        karmada-app: etcd
        karmada-etcd: {{ .StatefulSetName }}
    tolerations:
    - operator: Exists
    spec:
      automountServiceAccountToken: false
      containers:
      - name: etcd
        image: {{ .Image }}
        imagePullPolicy: IfNotPresent
        command:
        - /usr/local/bin/etcd
        - --name={{ .StatefulSetName }}0
        - --listen-client-urls= https://0.0.0.0:{{ .EtcdListenClientPort }}
        - --listen-peer-urls=http://0.0.0.0:{{ .EtcdListenPeerPort }}
        - --advertise-client-urls=https://{{ .EtcdClientService }}.{{ .Namespace }}.svc.cluster.local:{{ .EtcdListenClientPort }}
        - --initial-cluster={{ .StatefulSetName }}0=http://{{ .StatefulSetName }}-0.{{ .EtcdPeerServiceName }}.{{ .Namespace }}.svc.cluster.local:{{ .EtcdListenPeerPort }}
        - --initial-cluster-state=new
        - --client-cert-auth=true
        - --trusted-ca-file=/etc/karmada/pki/etcd/etcd-ca.crt
        - --cert-file=/etc/karmada/pki/etcd/etcd-server.crt
        - --key-file=/etc/karmada/pki/etcd/etcd-server.key
        - --data-dir=/var/lib/etcd
        - --snapshot-count=10000
        - --log-level=debug
        livenessProbe:
          exec:
            command:
            - /bin/sh
            - -ec
            - etcdctl get /registry --prefix --keys-only --endpoints https://127.0.0.1:{{ .EtcdListenClientPort }} --cacert=/etc/karmada/pki/etcd/etcd-ca.crt --cert=/etc/karmada/pki/etcd/etcd-server.crt --key=/etc/karmada/pki/etcd/etcd-server.key
          failureThreshold: 3
          initialDelaySeconds: 600
          periodSeconds: 60
          successThreshold: 1
          timeoutSeconds: 10
        ports:
        - containerPort: {{ .EtcdListenClientPort }}
          name: client
          protocol: TCP
        - containerPort: {{ .EtcdListenPeerPort }}
          name: server
          protocol: TCP
        volumeMounts:
        - mountPath: /var/lib/etcd
          name: etcd-data
        - mountPath: /etc/karmada/pki/etcd
          name: etcd-cert
      volumes:
      - name: etcd-cert
        secret:
          secretName: {{ .CertsSecretName }}
      - name: etcd-data
        emptyDir: {}
`
	KarmadaEtcdStatefulSetHA = `
apiVersion: apps/v1
kind: StatefulSet
metadata:
  labels:
    karmada-app: etcd
    app.kubernetes.io/managed-by: karmada-operator
  namespace: {{ .Namespace }}
  name: {{ .StatefulSetName }}
spec:
  replicas: {{ .Replicas }}
  serviceName: {{ .StatefulSetName }}
  podManagementPolicy: Parallel
  selector:
    matchLabels:
      karmada-app: etcd
      karmada-etcd: {{ .StatefulSetName }}
  template:
    metadata:
      labels:
        karmada-app: etcd
        karmada-etcd: {{ .StatefulSetName }}
    spec:
      containers:
        - name: etcd
          image: {{ .Image }}
          imagePullPolicy: IfNotPresent
          ports:
            - containerPort: {{ .EtcdListenPeerPort }}
              name: peer
              protocol: TCP
            - containerPort: {{ .EtcdListenClientPort }}
              name: client
              protocol: TCP

          env:
            - name: INITIAL_CLUSTER_SIZE
              value: "{{ .Replicas }}"
            - name: MY_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
            - name: POD_IP
              valueFrom:
                fieldRef:
                  fieldPath: status.podIP
            - name: SET_NAME
              value: "{{ .StatefulSetName }}"
          command:
            - /bin/sh
            - -ec
            - |
              HOSTNAME=$(hostname)

              eps() {
                  EPS=""
                  for i in $(seq 0 $((${INITIAL_CLUSTER_SIZE} - 1))); do
                      EPS="${EPS}${EPS:+,}https://${SET_NAME}-${i}.${SET_NAME}.${MY_NAMESPACE}.svc.{{ .ClusterDomain }}:{{ .EtcdListenClientPort }}"
                  done
                  echo ${EPS}
              }

              member_hash() {
                 ETCDCTL_API=3 etcdctl member list | grep -w "$HOSTNAME" | awk '{ print $1}' | awk -F "," '{ print $1}'
              }

              initial_peers() {
                  PEERS=""
                  for i in $(seq 0 $((${INITIAL_CLUSTER_SIZE} - 1))); do
                    PEERS="${PEERS}${PEERS:+,}${SET_NAME}-${i}=https://${SET_NAME}-${i}.${SET_NAME}.${MY_NAMESPACE}.svc.{{ .ClusterDomain }}:{{ .EtcdListenPeerPort }}"
                  done
                  echo ${PEERS}
              }

              # etcd-SET_ID
              SET_ID=${HOSTNAME##*-}

              # adding a new member to existing cluster (assuming all initial pods are available)
              if [ "${SET_ID}" -ge ${INITIAL_CLUSTER_SIZE} ]; then
                  # export ETCDCTL_ENDPOINTS=$(eps)
                  # member already added?

                  MEMBER_HASH=$(member_hash)
                  if [ -n "${MEMBER_HASH}" ]; then
                      # the member hash exists but for some reason etcd failed
                      # as the datadir has not be created, we can remove the member
                      # and retrieve new hash
                      echo "Remove member ${MEMBER_HASH}"
                    ETCDCTL_API=3 etcdctl --endpoints=$(eps) member remove ${MEMBER_HASH}
                  fi

                  echo "Adding new member"

                  echo "ETCDCTL_API=3 etcdctl --endpoints=$(eps) member add ${HOSTNAME} --peer-urls=https://${HOSTNAME}.${SET_NAME}.${MY_NAMESPACE}.svc.{{ .ClusterDomain }}:{{ .EtcdListenPeerPort }}"
                  ETCDCTL_API=3 etcdctl member --endpoints=$(eps) add ${HOSTNAME} --peer-urls=https://${HOSTNAME}.${SET_NAME}.${MY_NAMESPACE}.svc.{{ .ClusterDomain }}:{{ .EtcdListenPeerPort }} | grep "^ETCD_" > /var/run/etcd/new_member_envs

                  if [ $? -ne 0 ]; then
                      echo "member add ${HOSTNAME} error."
                      rm -f /var/run/etcd/new_member_envs
                      exit 1
                  fi

                  echo "==> Loading env vars of existing cluster..."
                  sed -ie "s/^/export /" /var/run/etcd/new_member_envs
                  cat /var/run/etcd/new_member_envs
                  . /var/run/etcd/new_member_envs

                  echo "etcd --name ${HOSTNAME} --initial-advertise-peer-urls ${ETCD_INITIAL_ADVERTISE_PEER_URLS} --listen-peer-urls https://${POD_IP}:{{ .EtcdListenPeerPort }} --listen-client-urls https://${POD_IP}:{{ .EtcdListenClientPort }},https://127.0.0.1:{{ .EtcdListenClientPort }} --advertise-client-urls https://${HOSTNAME}.${SET_NAME}.${MY_NAMESPACE}.svc.{{ .ClusterDomain }}:{{ .EtcdListenClientPort }} --data-dir /var/run/etcd/default.etcd --initial-cluster ${ETCD_INITIAL_CLUSTER} --initial-cluster-state ${ETCD_INITIAL_CLUSTER_STATE}"

                  exec etcd --listen-peer-urls https://${POD_IP}:{{ .EtcdListenPeerPort }} \
                      --listen-client-urls https://${POD_IP}:{{ .EtcdListenClientPort }},https://127.0.0.1:{{ .EtcdListenClientPort }} \
                      --advertise-client-urls https://${HOSTNAME}.${SET_NAME}.${MY_NAMESPACE}.svc.{{ .ClusterDomain }}:{{ .EtcdListenClientPort }} \
                      --client-cert-auth=true \
                      --trusted-ca-file=/etc/karmada/pki/etcd/etcd-ca.crt \
                      --cert-file=/etc/karmada/pki/etcd/etcd-server.crt \
                      --key-file=/etc/karmada/pki/etcd/etcd-server.key \
                      --peer-client-cert-auth=true \
                      --peer-trusted-ca-file=/etc/karmada/pki/etcd/etcd-ca.crt \
                      --peer-cert-file=/etc/karmada/pki/etcd/etcd-server.crt \
                      --peer-key-file=/etc/karmada/pki/etcd/etcd-server.key \
                      --data-dir /var/run/etcd/default.etcd
              fi

              for i in $(seq 0 $((${INITIAL_CLUSTER_SIZE} - 1))); do
                  while true; do
                      echo "Waiting for ${SET_NAME}-${i}.${SET_NAME}.${MY_NAMESPACE}.svc.{{ .ClusterDomain }} to come up"
                      ping -W 1 -c 1 ${SET_NAME}-${i}.${SET_NAME}.${MY_NAMESPACE}.svc.{{ .ClusterDomain }} > /dev/null && break
                      sleep 1s
                  done
              done

              echo "join member ${HOSTNAME}"
              # join member
              exec etcd --name ${HOSTNAME} \
                  --initial-advertise-peer-urls https://${HOSTNAME}.${SET_NAME}.${MY_NAMESPACE}.svc.{{ .ClusterDomain }}:{{ .EtcdListenPeerPort }} \
                  --listen-peer-urls https://${POD_IP}:{{ .EtcdListenPeerPort }} \
                  --listen-client-urls https://${POD_IP}:{{ .EtcdListenClientPort }},https://127.0.0.1:{{ .EtcdListenClientPort }} \
                  --advertise-client-urls https://${HOSTNAME}.${SET_NAME}.${MY_NAMESPACE}.svc.{{ .ClusterDomain }}:{{ .EtcdListenClientPort }} \
                  --initial-cluster-token etcd-cluster-1 \
                  --data-dir /var/run/etcd/default.etcd \
                  --initial-cluster $(initial_peers) \
                  --client-cert-auth=true \
                  --trusted-ca-file=/etc/karmada/pki/etcd/etcd-ca.crt \
                  --cert-file=/etc/karmada/pki/etcd/etcd-server.crt \
                  --key-file=/etc/karmada/pki/etcd/etcd-server.key \
                  --peer-client-cert-auth=true \
                  --peer-trusted-ca-file=/etc/karmada/pki/etcd/etcd-ca.crt \
                  --peer-cert-file=/etc/karmada/pki/etcd/etcd-server.crt \
                  --peer-key-file=/etc/karmada/pki/etcd/etcd-server.key \
                  --initial-cluster-state new
          lifecycle:
            preStop:
              exec:
                command:
                  - /bin/sh
                  - -ec
                  - |
                    HOSTNAME=$(hostname)

                    member_hash() {
                       ETCDCTL_API=3 etcdctl member list | grep -w "$HOSTNAME" | awk '{ print $1}' | awk -F "," '{ print $1}'
                    }

                    eps() {
                        EPS=""
                        for i in $(seq 0 $((${INITIAL_CLUSTER_SIZE} - 1))); do
                            EPS="${EPS}${EPS:+,}https://${SET_NAME}-${i}.${SET_NAME}.${MY_NAMESPACE}.svc.{{ .ClusterDomain }}:{{ .EtcdListenClientPort }}"
                        done
                        echo ${EPS}
                    }

                    export ETCDCTL_ENDPOINTS=$(eps)
                    SET_ID=${HOSTNAME##*-}

                    # Removing member from cluster
                    if [ "${SET_ID}" -ge ${INITIAL_CLUSTER_SIZE} ]; then
                        echo "Removing ${HOSTNAME} from etcd cluster"
                        ETCDCTL_API=3 etcdctl member remove $(member_hash)
                        if [ $? -eq 0 ]; then
                            # Remove everything otherwise the cluster will no longer scale-up
                            rm -rf /var/run/etcd/*
                        fi
                    fi
          livenessProbe:
            exec:
              command:
              - /bin/sh
              - -ec
              - etcdctl get /registry --prefix --keys-only --endpoints https://127.0.0.1:{{ .EtcdListenClientPort }} --cacert=/etc/karmada/pki/etcd/etcd-ca.crt --cert=/etc/karmada/pki/etcd/etcd-server.crt --key=/etc/karmada/pki/etcd/etcd-server.key
            failureThreshold: 3
            initialDelaySeconds: 600
            periodSeconds: 60
            successThreshold: 1
            timeoutSeconds: 10
          volumeMounts:
            - mountPath: /var/run/etcd
              name: {{ .VolumeName }}
            - mountPath: /etc/karmada/pki/etcd
              name: etcd-cert
      volumes:
        - name: etcd-cert
          secret:
            secretName: {{ .CertsSecretName }}
  volumeClaimTemplates:
    - metadata:
        name: {{ .VolumeName }}
      spec:
        storageClassName: {{ .StorageClassName }}
        accessModes: [{{range  $accessMode := .AccessModes}}"{{$accessMode}}",{{- end }}]
        resources:
          requests:
            # upstream recommended max is 700M
            storage: {{ .StorageSize }}
`

	// KarmadaEtcdClientService is karmada etcd client service manifest
	KarmadaEtcdClientService = `
apiVersion: v1
kind: Service
metadata:
  labels:
    karmada-app: etcd
    app.kubernetes.io/managed-by: karmada-operator
  name: {{ .ServiceName }}
  namespace: {{ .Namespace }}
spec:
  ports:
  - name: client
    port: {{ .EtcdListenClientPort }}
    protocol: TCP
    targetPort: {{ .EtcdListenClientPort }}
  selector:
    karmada-app: etcd
    karmada-etcd: {{ .StatefulSetName }}
  type: ClusterIP
 `

	// KarmadaEtcdPeerService is karmada etcd peer Service manifest
	KarmadaEtcdPeerService = `
 apiVersion: v1
 kind: Service
 metadata:
   labels:
     karmada-app: etcd
     app.kubernetes.io/managed-by: karmada-operator
   name: {{ .ServiceName }}
   namespace: {{ .Namespace }}
 spec:
   clusterIP: None
   ports:
   - name: client
     port: {{ .EtcdListenClientPort }}
     protocol: TCP
     targetPort: {{ .EtcdListenClientPort }}
   - name: server
     port: {{ .EtcdListenPeerPort }}
     protocol: TCP
     targetPort: {{ .EtcdListenPeerPort }}
   type: ClusterIP
   selector:
     karmada-app: etcd
     karmada-etcd: {{ .StatefulSetName }}
   publishNotReadyAddresses: true
  `
)
