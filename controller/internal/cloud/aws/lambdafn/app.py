import json
import boto3
import os
import re
import urllib.request

secretsmanager = boto3.client('secretsmanager')
TOKEN_SECRET_NAME = os.environ['FLUX2_WEBHOOK_TOKEN_SECRET_NAME']
WEBHOOK_MAP_SECRET_NAME = os.environ['REPOS_MAPPING']
default_token = None
webhook_map = None


def get_global_token():
    global default_token
    if not default_token:
        response = secretsmanager.get_secret_value(SecretId=TOKEN_SECRET_NAME)
        default_token = response['SecretString']
    return default_token


def get_webhook_map():
    global webhook_map
    if not webhook_map:
        response = secretsmanager.get_secret_value(SecretId=WEBHOOK_MAP_SECRET_NAME)
        webhook_map = json.loads(response['SecretString'])
    return webhook_map


def parse_receiver_key(key):
    """Extract cluster and receiver name from a key like 'cluster::receiver' or just 'receiver'."""
    if '::' in key:
        cluster, receiver = key.split('::', 1)
        return cluster, receiver
    return '', key


def process_ecr_push_event(detail):
    repository = detail['repository-name']
    image_digest = detail['image-digest']
    image_tag = detail['image-tag']

    print(json.dumps({
        'message': 'ECR push event received',
        'repository': repository,
        'image_digest': image_digest,
        'image_tag': image_tag
    }))


def make_request(webhook_url, repository, receiver, cluster, image_tag, headers):
    if not webhook_url:
        print(json.dumps({
            'message': 'No webhook URL configured',
            'repository': repository,
            'receiver': receiver,
            **({'cluster': cluster} if cluster else {}),
        }))
        return

    req = urllib.request.Request(
        webhook_url,
        data=json.dumps({}).encode('utf-8'),
        headers=headers,
        method='POST',
    )
    req.add_header('Content-Type', 'application/json')
    try:
        response = urllib.request.urlopen(req)
        print(json.dumps({
            'message': 'Webhook called',
            'repository': repository,
            'receiver': receiver,
            'image_tag': image_tag,
            'status_code': response.status,
            **({'cluster': cluster} if cluster else {}),
        }))
    except urllib.error.HTTPError as e:
        print(json.dumps({
            'message': 'Webhook request failed',
            'repository': repository,
            'receiver': receiver,
            'image_tag': image_tag,
            'status_code': e.code,
            'reason': str(e.reason),
            **({'cluster': cluster} if cluster else {}),
        }))


def call_flux_webhook(repository, image_tag):
    wh_map = get_webhook_map()

    if repository not in wh_map:
        print(json.dumps({
            'message': 'No mapping found for repository',
            'repository': repository,
            'image_tag': image_tag,
        }))
        return

    repo_data = wh_map[repository]
    for key, data in repo_data.items():
        cluster, receiver = parse_receiver_key(key)
        webhook_urls = data.get('webhook', [])
        token = data.get('token', get_global_token())
        regex = data.get('regex', '.*')

        if not regex or not re.match(regex, image_tag):
            print(json.dumps({
                'message': 'Tag filtered by regex',
                'repository': repository,
                'receiver': receiver,
                'image_tag': image_tag,
                'regex': regex,
                **({'cluster': cluster} if cluster else {}),
            }))
            continue

        for webhook in webhook_urls:
            headers = {'Authorization': f'Bearer {token}'}
            make_request(webhook, repository, receiver, cluster, image_tag, headers)


def lambda_handler(event, context):
    record = event['Records'][0]
    message = json.loads(record['body'])
    detail = message['detail']

    process_ecr_push_event(detail)
    call_flux_webhook(detail['repository-name'], detail['image-tag'])

    return {
        'statusCode': 200,
        'body': json.dumps('Event processed successfully')
    }
