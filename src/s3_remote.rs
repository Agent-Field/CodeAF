//! Small, command-scoped S3-compatible transport using SigV4 presigned URLs.

use anyhow::Context;
use rusty_s3::actions::{GetObject, HeadObject, PutObject};
use rusty_s3::{Bucket, Credentials, S3Action, UrlStyle};
use serde::{Deserialize, Serialize};
use std::time::Duration;
use url::Url;

const SIGNED_URL_TTL: Duration = Duration::from_secs(5 * 60);
const REQUEST_TIMEOUT: Duration = Duration::from_secs(60);

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct S3Spec {
    pub bucket: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub prefix: String,
    pub endpoint: String,
    pub region: String,
    pub path_style: bool,
}

impl S3Spec {
    pub fn from_uri(value: &str) -> anyhow::Result<Self> {
        let raw = value
            .strip_prefix("s3://")
            .context("S3 remote must use s3://")?;
        if let Some((_, raw_path)) = raw.split_once('/') {
            validate_prefix(raw_path.trim_matches('/'))?;
        }
        let uri = Url::parse(value).context("invalid S3 remote URI")?;
        anyhow::ensure!(uri.scheme() == "s3", "S3 remote must use s3://");
        anyhow::ensure!(
            uri.username().is_empty()
                && uri.password().is_none()
                && uri.port().is_none()
                && uri.query().is_none()
                && uri.fragment().is_none(),
            "S3 remote URI must not contain credentials, port, query, or fragment"
        );
        let bucket = uri.host_str().context("S3 remote is missing a bucket")?;
        validate_bucket(bucket)?;
        let prefix = uri.path().trim_matches('/').to_owned();
        validate_prefix(&prefix)?;
        let region = std::env::var("AWS_REGION")
            .or_else(|_| std::env::var("AWS_DEFAULT_REGION"))
            .unwrap_or_else(|_| "us-east-1".to_owned());
        anyhow::ensure!(!region.trim().is_empty(), "S3 region cannot be empty");
        let custom_endpoint = std::env::var("AGIT_S3_ENDPOINT").ok();
        let endpoint = custom_endpoint
            .clone()
            .unwrap_or_else(|| format!("https://s3.{region}.amazonaws.com"));
        let endpoint_url = Url::parse(&endpoint).context("invalid AGIT_S3_ENDPOINT")?;
        anyhow::ensure!(
            matches!(endpoint_url.scheme(), "http" | "https"),
            "S3 endpoint must use http or https"
        );
        if endpoint_url.scheme() == "http" {
            anyhow::ensure!(
                std::env::var("AGIT_S3_ALLOW_HTTP").as_deref() == Ok("1"),
                "plain HTTP S3 endpoint requires AGIT_S3_ALLOW_HTTP=1"
            );
        }
        let path_style = match std::env::var("AGIT_S3_PATH_STYLE").as_deref() {
            Ok("1" | "true") => true,
            Ok("0" | "false") => false,
            Ok(_) => anyhow::bail!("AGIT_S3_PATH_STYLE must be true or false"),
            Err(_) => custom_endpoint.is_some(),
        };
        Ok(Self {
            bucket: bucket.to_owned(),
            prefix,
            endpoint,
            region,
            path_style,
        })
    }

    pub fn validate(&self) -> anyhow::Result<()> {
        validate_bucket(&self.bucket)?;
        validate_prefix(&self.prefix)?;
        let endpoint = Url::parse(&self.endpoint).context("invalid S3 endpoint")?;
        anyhow::ensure!(
            matches!(endpoint.scheme(), "http" | "https") && endpoint.host_str().is_some(),
            "invalid S3 endpoint"
        );
        anyhow::ensure!(!self.region.trim().is_empty(), "S3 region cannot be empty");
        Ok(())
    }

    pub fn display(&self) -> String {
        if self.prefix.is_empty() {
            format!("s3://{}", self.bucket)
        } else {
            format!("s3://{}/{}", self.bucket, self.prefix)
        }
    }
}

pub(crate) struct S3Session {
    bucket: Bucket,
    credentials: Credentials,
    agent: ureq::Agent,
    prefix: String,
    writer: bool,
    expected_head_etag: Option<Option<String>>,
}

impl S3Session {
    pub fn open(spec: &S3Spec, namespace: &str) -> anyhow::Result<Self> {
        spec.validate()?;
        let credentials = Credentials::from_env().context(
            "S3 credentials are missing; set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY",
        )?;
        let endpoint = Url::parse(&spec.endpoint)?;
        let style = if spec.path_style {
            UrlStyle::Path
        } else {
            UrlStyle::VirtualHost
        };
        let bucket = Bucket::new(endpoint, style, spec.bucket.clone(), spec.region.clone())
            .map_err(|error| anyhow::anyhow!("configure S3 bucket: {error:?}"))?;
        let prefix = if spec.prefix.is_empty() {
            namespace.to_owned()
        } else {
            format!("{}/{namespace}", spec.prefix)
        };
        let agent: ureq::Agent = ureq::Agent::config_builder()
            .timeout_global(Some(REQUEST_TIMEOUT))
            .max_redirects(0)
            .build()
            .into();
        Ok(Self {
            bucket,
            credentials,
            agent,
            prefix,
            writer: false,
            expected_head_etag: None,
        })
    }

    pub fn begin_writer(&mut self) -> anyhow::Result<()> {
        anyhow::ensure!(!self.writer, "remote writer session is already active");
        self.expected_head_etag = Some(self.head("HEAD")?);
        self.writer = true;
        Ok(())
    }

    pub fn exists(&self, key: &str) -> anyhow::Result<bool> {
        Ok(self.head(key)?.is_some())
    }

    pub fn read(&self, key: &str, limit: u64) -> anyhow::Result<Vec<u8>> {
        let object = self.object_key(key);
        let action = GetObject::new(&self.bucket, Some(&self.credentials), &object);
        let url = action.sign(SIGNED_URL_TTL);
        let mut response = self
            .agent
            .get(url.as_str())
            .call()
            .map_err(|error| map_error(error, key))?;
        if let Some(length) = response
            .headers()
            .get("content-length")
            .and_then(|value| value.to_str().ok())
            .and_then(|value| value.parse::<u64>().ok())
        {
            anyhow::ensure!(length <= limit, "remote value exceeds its size limit");
        }
        response
            .body_mut()
            .with_config()
            .limit(limit)
            .read_to_vec()
            .map_err(|error| anyhow::anyhow!("read S3 object {key}: {error}"))
    }

    pub fn write(&mut self, key: &str, bytes: &[u8]) -> anyhow::Result<()> {
        anyhow::ensure!(self.writer, "remote write requires a writer session");
        if key.starts_with("objects/") {
            return match self.put(key, bytes, PutCondition::Create) {
                Ok(_) | Err(PutError::AlreadyExists) => Ok(()),
                Err(error) => Err(error.into_anyhow(key)),
            };
        }
        if key == "HEAD" {
            let condition = match self
                .expected_head_etag
                .as_ref()
                .context("S3 writer has no expected HEAD state")?
            {
                Some(etag) => PutCondition::Match(etag),
                None => PutCondition::Create,
            };
            let etag = self
                .put(key, bytes, condition)
                .map_err(|error| error.into_anyhow(key))?;
            self.expected_head_etag = Some(Some(etag));
            return Ok(());
        }
        self.put(key, bytes, PutCondition::Any)
            .map(|_| ())
            .map_err(|error| error.into_anyhow(key))
    }

    pub fn has_objects(&self, ids: &[crate::model::ObjectId]) -> anyhow::Result<Vec<bool>> {
        ids.iter()
            .map(|id| self.exists(&crate::remote::object_key(id)))
            .collect()
    }

    fn head(&self, key: &str) -> anyhow::Result<Option<String>> {
        let object = self.object_key(key);
        let action = HeadObject::new(&self.bucket, Some(&self.credentials), &object);
        let url = action.sign(SIGNED_URL_TTL);
        match self.agent.head(url.as_str()).call() {
            Ok(response) => Ok(Some(
                response
                    .headers()
                    .get("etag")
                    .and_then(|value| value.to_str().ok())
                    .context("S3 response is missing ETag")?
                    .to_owned(),
            )),
            Err(ureq::Error::StatusCode(404)) => Ok(None),
            Err(error) => Err(map_error(error, key)),
        }
    }

    fn put(
        &self,
        key: &str,
        bytes: &[u8],
        condition: PutCondition<'_>,
    ) -> Result<String, PutError> {
        let object = self.object_key(key);
        let mut action = PutObject::new(&self.bucket, Some(&self.credentials), &object);
        let header = match condition {
            PutCondition::Any => None,
            PutCondition::Create => Some(("if-none-match", "*")),
            PutCondition::Match(etag) => Some(("if-match", etag)),
        };
        if let Some((name, value)) = header {
            action.headers_mut().insert(name, value);
        }
        let url = action.sign(SIGNED_URL_TTL);
        let mut request = self.agent.put(url.as_str());
        if let Some((name, value)) = header {
            request = request.header(name, value);
        }
        match request.send(bytes) {
            Ok(response) => Ok(response
                .headers()
                .get("etag")
                .and_then(|value| value.to_str().ok())
                .unwrap_or_default()
                .to_owned()),
            Err(ureq::Error::StatusCode(409 | 412))
                if matches!(condition, PutCondition::Create) =>
            {
                Err(PutError::AlreadyExists)
            }
            Err(ureq::Error::StatusCode(409 | 412)) => Err(PutError::PreconditionFailed),
            Err(error) => Err(PutError::Transport(error.to_string())),
        }
    }

    fn object_key(&self, key: &str) -> String {
        format!("{}/{key}", self.prefix)
    }
}

#[derive(Clone, Copy)]
enum PutCondition<'a> {
    Any,
    Create,
    Match(&'a str),
}

enum PutError {
    AlreadyExists,
    PreconditionFailed,
    Transport(String),
}

impl PutError {
    fn into_anyhow(self, key: &str) -> anyhow::Error {
        match self {
            Self::AlreadyExists => anyhow::anyhow!("S3 object already exists: {key}"),
            Self::PreconditionFailed => anyhow::anyhow!(
                "S3 remote advanced concurrently while publishing {key}; pull before retrying"
            ),
            Self::Transport(message) => anyhow::anyhow!("write S3 object {key}: {message}"),
        }
    }
}

fn map_error(error: ureq::Error, key: &str) -> anyhow::Error {
    match error {
        ureq::Error::StatusCode(401) => anyhow::anyhow!("S3 credentials were rejected"),
        ureq::Error::StatusCode(403) => anyhow::anyhow!("S3 access was forbidden for {key}"),
        ureq::Error::StatusCode(404) => anyhow::anyhow!("S3 object was not found: {key}"),
        ureq::Error::StatusCode(status) => {
            anyhow::anyhow!("S3 request for {key} failed with HTTP {status}")
        }
        other => anyhow::anyhow!("S3 request for {key} failed: {other}"),
    }
}

fn validate_bucket(bucket: &str) -> anyhow::Result<()> {
    anyhow::ensure!(
        (3..=255).contains(&bucket.len())
            && !bucket.starts_with('.')
            && !bucket.ends_with('.')
            && bucket
                .bytes()
                .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'.' | b'-')),
        "invalid S3 bucket name"
    );
    Ok(())
}

fn validate_prefix(prefix: &str) -> anyhow::Result<()> {
    anyhow::ensure!(prefix.len() <= 768, "S3 prefix is too long");
    anyhow::ensure!(
        prefix
            .split('/')
            .all(|part| !matches!(part, "." | "..") && !part.is_empty())
            || prefix.is_empty(),
        "S3 prefix contains an unsafe path component"
    );
    anyhow::ensure!(
        prefix
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'/' | b'.' | b'-' | b'_')),
        "S3 prefix contains unsupported characters"
    );
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_provider_neutral_s3_uris_without_credentials() {
        std::env::remove_var("AGIT_S3_ENDPOINT");
        std::env::remove_var("AGIT_S3_PATH_STYLE");
        let spec = S3Spec::from_uri("s3://my-bucket/agit/workspaces").unwrap();
        assert_eq!(spec.bucket, "my-bucket");
        assert_eq!(spec.prefix, "agit/workspaces");
        assert_eq!(spec.display(), "s3://my-bucket/agit/workspaces");
        assert!(!spec.path_style);
        assert!(S3Spec::from_uri("s3://key:secret@bucket/path").is_err());
        assert!(S3Spec::from_uri("s3://bucket/a/../b").is_err());
    }
}
