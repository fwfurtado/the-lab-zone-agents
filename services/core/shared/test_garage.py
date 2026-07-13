"""Testes do GarageClient.

O par (aioboto3 + moto) tem um gap de compatibilidade real nesta combinação de
versões (chunked transfer encoding do aiobotocore não é entendido pelo stub
síncrono do moto — confirmado ao tentar, não suposto). Em vez de um S3 fake
completo, testamos a FRONTEIRA: que `put_text` chama `put_object` com Bucket/
Key/Body/ContentType corretos. Que `session.client("s3", ...)` + `put_object`
é a assinatura real da API foi confirmado à parte, tentando conectar a um
endpoint inexistente (falha em EndpointConnectionError, não em TypeError de
assinatura) — o que garante que o mock aqui não está testando uma API que não
existe.
"""

from unittest.mock import AsyncMock, MagicMock

import pytest

from shared.garage import GarageClient


def _client_with_mocked_s3():
    """GarageClient com session.client(...) trocado por um async context
    manager mockado. Devolve (garage_client, s3_mock) para inspecionar as
    chamadas."""
    garage = GarageClient(
        endpoint="garage.data.svc.cluster.local:3900",  # sem esquema, como o Go
        access_key="ak",
        secret_key="sk",
        bucket="triage-reports",
        region="garage",
    )
    s3_mock = AsyncMock()
    cm = MagicMock()
    cm.__aenter__ = AsyncMock(return_value=s3_mock)
    cm.__aexit__ = AsyncMock(return_value=False)
    garage._session.client = MagicMock(return_value=cm)
    return garage, s3_mock


@pytest.mark.asyncio
async def test_put_text_chama_put_object_com_bucket_key_body():
    garage, s3 = _client_with_mocked_s3()
    await garage.put_text("confirmations/data/x/20260711__abc.md", "---\nk: v\n---\n")

    s3.put_object.assert_awaited_once()
    _, kwargs = s3.put_object.call_args
    assert kwargs["Bucket"] == "triage-reports"
    assert kwargs["Key"] == "confirmations/data/x/20260711__abc.md"
    assert kwargs["Body"] == b"---\nk: v\n---\n"  # codificado utf-8


@pytest.mark.asyncio
async def test_put_text_content_type_default_e_markdown():
    garage, s3 = _client_with_mocked_s3()
    await garage.put_text("k.md", "corpo")

    _, kwargs = s3.put_object.call_args
    assert kwargs["ContentType"] == "text/markdown; charset=utf-8"


@pytest.mark.asyncio
async def test_put_text_usa_as_credenciais_do_client():
    garage, _ = _client_with_mocked_s3()
    await garage.put_text("k.md", "corpo")

    _, kwargs = garage._session.client.call_args
    assert kwargs["endpoint_url"] == "http://garage.data.svc.cluster.local:3900"
    assert kwargs["aws_access_key_id"] == "ak"
    assert kwargs["aws_secret_access_key"] == "sk"
    assert kwargs["region_name"] == "garage"


def test_endpoint_sem_esquema_mais_use_ssl_monta_https():
    """endpoint é host:port SEM esquema (mesma convenção do Go/minio-go);
    use_ssl decide http vs https — nunca embutido no valor de endpoint, senão
    o mesmo literal do manifesto do triage-webhook (Go) quebraria aqui."""
    garage = GarageClient(
        endpoint="garage.data.svc.cluster.local:3900",
        access_key="ak",
        secret_key="sk",
        bucket="triage-reports",
        use_ssl=True,
    )
    assert garage._endpoint_url == "https://garage.data.svc.cluster.local:3900"


def test_endpoint_use_ssl_default_e_false():
    garage = GarageClient(
        endpoint="garage.data.svc.cluster.local:3900",
        access_key="ak",
        secret_key="sk",
        bucket="triage-reports",
    )
    assert garage._endpoint_url == "http://garage.data.svc.cluster.local:3900"


@pytest.mark.asyncio
async def test_put_text_codifica_utf8_com_acentos():
    garage, s3 = _client_with_mocked_s3()
    await garage.put_text("k.md", "não foi o pod de teste")

    _, kwargs = s3.put_object.call_args
    assert kwargs["Body"] == "não foi o pod de teste".encode()


@pytest.mark.asyncio
async def test_assinatura_real_da_api_e_a_que_o_mock_espelha():
    """Confirma, sem servidor real, que `session.client('s3', endpoint_url=...,
    aws_access_key_id=..., aws_secret_access_key=..., region_name=...)` é a
    assinatura real do aioboto3 instalado — ao tentar conectar a uma porta que
    recusa conexão, falha em erro de REDE (o cliente foi montado certo), não em
    TypeError de kwargs desconhecidos. Prova que os kwargs usados no mock acima
    não estão testando uma API que não existe."""
    import aioboto3
    from botocore.config import Config
    from botocore.exceptions import EndpointConnectionError

    session = aioboto3.Session()
    with pytest.raises(EndpointConnectionError):
        async with session.client(
            "s3",
            endpoint_url="http://localhost:1",
            aws_access_key_id="x",
            aws_secret_access_key="y",
            region_name="garage",
            config=Config(connect_timeout=1, retries={"max_attempts": 1}),
        ) as s3:
            await s3.put_object(Bucket="b", Key="k", Body=b"x")
