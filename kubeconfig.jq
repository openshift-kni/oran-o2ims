{
  "apiVersion": "v1",
  "kind": "Config",
  "clusters": [
    {
      "name": .name,
      "cluster": {
        "server": .serviceUri,
        "certificate-authority-data": .extensions.profileData.cluster_ca_cert
      }
    }
  ],
  "users": [
    {
      "name": .extensions.profileData.admin_user,
      "user": {
        "client-certificate-data": .extensions.profileData.admin_client_cert,
        "client-key-data": .extensions.profileData.admin_client_key
      }
    }
  ],
  "contexts": [
    {
      "name": .name,
      "context": {
        "cluster": .name,
        "user": .extensions.profileData.admin_user
      }
    }
  ],
  "current-context": .name
}
