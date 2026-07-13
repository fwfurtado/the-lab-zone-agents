"""Cliente Garage mínimo: escreve UM objeto de texto por vez.

Único escritor Python de Garage neste repo — até aqui, toda escrita vinha da
borda Go (`triage-webhook`, via minio-go) ou de jobs Argo via `rclone`
(bulk, ver `the-lab-zone-dockerfiles`). O listener de feedback do Slack
(ADR-0014) escreve UM artefato pequeno por interação, síncrono com o clique do
usuário — forma de operação diferente de ambos: nem bulk (rclone), nem
já-tem-cliente-Go. Um cliente S3 assíncrono direto é o que essa forma pede.

`aioboto3` (não boto3 síncrono): o processo que chama isto é o bridge Socket
Mode do Slack (`shared/slack/app.py`), um único event loop asyncio de longa
duração — uma chamada S3 síncrona bloquearia esse loop e atrasaria toda
mensagem/interação em trânsito, não só a escrita da confirmação.

`endpoint` (não `endpoint_url`) é de propósito: recebe host:port SEM esquema,
igual à borda Go (GARAGE_ENDPOINT/GARAGE_USE_SSL, minio-go). Um único
vocabulário de env entre as duas linguagens que escrevem no mesmo bucket —
sem isso, alguém copiaria o valor do manifesto do triage-webhook Go pro do
slack-qa-bot Python e o cliente quebraria por falta de esquema na URL.

Não é um cliente geral: só PUT. Nem lista, nem lê, nem deleta — o listener não
precisa (não confirma duas vezes o mesmo clique verificando primeiro; escrever
de novo é o próprio mecanismo de correção, ADR-0014).
"""

from __future__ import annotations

import aioboto3


class GarageClient:
    def __init__(
        self,
        *,
        endpoint: str,
        access_key: str,
        secret_key: str,
        bucket: str,
        region: str = "garage",
        use_ssl: bool = False,
    ) -> None:
        """`endpoint` é host:port SEM esquema (ex.: `garage.data.svc.cluster.
        local:3900`) — mesma convenção da borda Go (GaragePublisher, minio-go),
        que também separa endpoint de use_ssl. Um único vocabulário de env
        entre as duas linguagens que falam com o mesmo bucket: quem configura
        o manifesto não precisa lembrar que uma delas quer esquema embutido e
        a outra não.
        """
        self._endpoint_url = f"{'https' if use_ssl else 'http'}://{endpoint}"
        self._access_key = access_key
        self._secret_key = secret_key
        self._bucket = bucket
        self._region = region
        self._session = aioboto3.Session()

    async def put_text(self, key: str, body: str, *, content_type: str = "text/markdown; charset=utf-8") -> None:
        """PUT idempotente — mesma chave sobrescreve. É esperado: o único
        caminho de reescrita em `confirmations/` é a mesma pessoa corrigindo a
        própria interação (ADR-0014), nunca um job automático."""
        async with self._session.client(
            "s3",
            endpoint_url=self._endpoint_url,
            aws_access_key_id=self._access_key,
            aws_secret_access_key=self._secret_key,
            region_name=self._region,
        ) as s3:
            await s3.put_object(
                Bucket=self._bucket,
                Key=key,
                Body=body.encode("utf-8"),
                ContentType=content_type,
            )
